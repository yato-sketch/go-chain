/*
 * sha256mem GPU Hash Validator
 * ============================
 * Computes sha256mem on CPU (OpenSSL) and GPU (OpenCL) with the same
 * input, then compares byte-by-byte to verify correctness.
 *
 * Build:
 *   clang -O2 -o validate_gpu validate_gpu.c -lssl -lcrypto -lOpenCL
 *
 * Copyright (c) 2024-2026 The Fairchain Contributors
 * Distributed under the MIT software license.
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <openssl/sha.h>

#ifdef __APPLE__
#include <OpenCL/opencl.h>
#else
#include <CL/cl.h>
#endif

#define SHA256MEM_SLOTS       4194304
#define SHA256MEM_FILL_CHAINS 8192
#define SHA256MEM_MIX_ROUNDS  2048

static void sha256mem_cpu(const uint8_t *data, size_t len, uint8_t out[32])
{
    uint8_t (*mem)[32] = malloc(SHA256MEM_SLOTS * 32);
    if (!mem) { memset(out, 0, 32); return; }

    SHA256(data, len, mem[0]);

    int spread = SHA256MEM_SLOTS / SHA256MEM_FILL_CHAINS;
    for (int j = 1; j < spread; j++)
        memcpy(mem[j], mem[0], 32);
    for (int i = 1; i < SHA256MEM_FILL_CHAINS; i++) {
        int base = i * spread;
        int prev = (i - 1) * spread;
        SHA256(mem[prev], 32, mem[base]);
        for (int j = 1; j < spread; j++)
            memcpy(mem[base + j], mem[base], 32);
    }

    uint8_t acc[32];
    memcpy(acc, mem[SHA256MEM_SLOTS - 1], 32);
    for (int i = 0; i < SHA256MEM_MIX_ROUNDS; i++) {
        uint32_t idx;
        memcpy(&idx, acc, 4);
        idx %= SHA256MEM_SLOTS;
        uint8_t buf[64];
        memcpy(buf, acc, 32);
        memcpy(buf + 32, mem[idx], 32);
        SHA256(buf, 64, acc);
    }

    SHA256(acc, 32, out);
    free(mem);
}

static void print_hex(const char *label, const uint8_t *data, int len)
{
    printf("  %s: ", label);
    for (int i = 0; i < len; i++)
        printf("%02x", data[i]);
    printf("\n");
}

static char *load_file(const char *path, size_t *len)
{
    FILE *f = fopen(path, "r");
    if (!f) { fprintf(stderr, "Cannot open: %s\n", path); exit(1); }
    fseek(f, 0, SEEK_END);
    *len = (size_t)ftell(f);
    fseek(f, 0, SEEK_SET);
    char *src = malloc(*len + 1);
    if (fread(src, 1, *len, f) != *len) { fprintf(stderr, "Read error\n"); exit(1); }
    src[*len] = '\0';
    fclose(f);
    return src;
}

#define CL_CHECK(call, msg) do { \
    cl_int _err = (call); \
    if (_err != CL_SUCCESS) { \
        fprintf(stderr, "OpenCL error: %s (%d)\n", msg, _err); \
        exit(1); \
    } \
} while(0)

/*
 * Modified kernel that writes back the final hash for worker 0.
 * We use the existing kernel but read back found_hash from worker 0.
 * Actually, the kernel writes to found_hash only on target match.
 * We need to add a way to extract the hash. Simplest: use 1 worker,
 * 1 hash, and modify the kernel to always write the hash.
 *
 * Instead, let's create a tiny wrapper kernel inline.
 */

int main(int argc, char **argv)
{
    printf("╔══════════════════════════════════════════════════════════════╗\n");
    printf("║         sha256mem GPU Hash Validator                       ║\n");
    printf("╠══════════════════════════════════════════════════════════════╣\n");
    printf("║  Computes same hash on CPU + GPU, compares byte-by-byte   ║\n");
    printf("║  Slots: %6d  MixRounds: %4d  (SHA256 per hop)        ║\n",
           SHA256MEM_SLOTS, SHA256MEM_MIX_ROUNDS);
    printf("╚══════════════════════════════════════════════════════════════╝\n\n");

    /* Build a test header (80 bytes, nonce=0) */
    uint8_t header[80];
    memset(header, 0, 80);
    header[0] = 0x01;

    /* ── CPU hash ──────────────────────────────────────────────────── */
    printf("  Computing CPU reference hash (this may take a moment)...\n");
    uint8_t cpu_hash[32];
    sha256mem_cpu(header, 80, cpu_hash);
    print_hex("CPU hash", cpu_hash, 32);

    /* Also dump first few fill slots for debugging */
    uint8_t (*cpu_mem)[32] = malloc(SHA256MEM_SLOTS * 32);
    SHA256(header, 80, cpu_mem[0]);
    {
        int spread = SHA256MEM_SLOTS / SHA256MEM_FILL_CHAINS;
        for (int j = 1; j < spread; j++)
            memcpy(cpu_mem[j], cpu_mem[0], 32);
        for (int i = 1; i < SHA256MEM_FILL_CHAINS; i++) {
            int base = i * spread;
            int prev = (i - 1) * spread;
            SHA256(cpu_mem[prev], 32, cpu_mem[base]);
            for (int j = 1; j < spread; j++)
                memcpy(cpu_mem[base + j], cpu_mem[base], 32);
        }
    }
    printf("\n  CPU fill phase check:\n");
    print_hex("  slot[0]   ", cpu_mem[0], 32);
    print_hex("  slot[1]   ", cpu_mem[1], 32);
    print_hex("  slot[last]", cpu_mem[SHA256MEM_SLOTS-1], 32);

    uint32_t cpu_first_idx;
    memcpy(&cpu_first_idx, cpu_mem[SHA256MEM_SLOTS-1], 4);
    cpu_first_idx %= SHA256MEM_SLOTS;
    printf("  CPU first mix idx (from last slot): %u\n\n", cpu_first_idx);
    free(cpu_mem);

    /* ── GPU setup ─────────────────────────────────────────────────── */
    cl_platform_id platform;
    cl_device_id device;
    cl_int err;

    CL_CHECK(clGetPlatformIDs(1, &platform, NULL), "get platform");
    CL_CHECK(clGetDeviceIDs(platform, CL_DEVICE_TYPE_GPU, 1, &device, NULL), "get device");

    char dev_name[256];
    clGetDeviceInfo(device, CL_DEVICE_NAME, sizeof(dev_name), dev_name, NULL);
    printf("  GPU: %s\n", dev_name);

    cl_context ctx = clCreateContext(NULL, 1, &device, NULL, NULL, &err);
    if (err != CL_SUCCESS) { fprintf(stderr, "create context: %d\n", err); return 1; }

    cl_command_queue queue = clCreateCommandQueue(ctx, device, 0, &err);
    if (err != CL_SUCCESS) { fprintf(stderr, "create queue: %d\n", err); return 1; }

    /* Load kernel — but we need to modify it to output the hash.
     * We'll append a small wrapper that writes the hash for gid==0. */
    size_t src_len;
    char *ksrc = load_file("sha256mem_gpu.cl", &src_len);

    /* Build a validation kernel that always writes the final hash to found_hash */
    const char *validation_kernel =
        "\n__kernel void sha256mem_validate(\n"
        "    __global const uchar *header,\n"
        "    __global uchar       *mem_pool,\n"
        "    __global uchar       *out_hash,\n"
        "    __global uchar       *out_slot0,\n"
        "    __global uchar       *out_slot1,\n"
        "    __global uchar       *out_slotlast,\n"
        "    __global uint        *out_first_idx\n"
        ")\n"
        "{\n"
        "    __global uchar (*mem)[32] = (__global uchar (*)[32])mem_pool;\n"
        "    uchar hdr[80];\n"
        "    for (int i = 0; i < 80; i++) hdr[i] = header[i];\n"
        "\n"
        "    /* Phase 1: Seed */\n"
        "    uchar slot[32];\n"
        "    sha256_80(hdr, slot);\n"
        "    for (int b = 0; b < 32; b++) mem[0][b] = slot[b];\n"
        "\n"
        "    /* Phase 2: Fast fill */\n"
        "    int spread = SHA256MEM_SLOTS / SHA256MEM_FILL_CHAINS;\n"
        "    for (int j = 1; j < spread; j++)\n"
        "        for (int b = 0; b < 32; b++) mem[j][b] = mem[0][b];\n"
        "    for (int i = 1; i < SHA256MEM_FILL_CHAINS; i++) {\n"
        "        int base = i * spread;\n"
        "        int prev = (i - 1) * spread;\n"
        "        uchar pdata[32];\n"
        "        for (int b = 0; b < 32; b++) pdata[b] = mem[prev][b];\n"
        "        sha256_32(pdata, slot);\n"
        "        for (int b = 0; b < 32; b++) mem[base][b] = slot[b];\n"
        "        for (int j = 1; j < spread; j++)\n"
        "            for (int b = 0; b < 32; b++) mem[base+j][b] = mem[base][b];\n"
        "    }\n"
        "\n"
        "    /* Dump debug slots */\n"
        "    for (int b = 0; b < 32; b++) out_slot0[b] = mem[0][b];\n"
        "    for (int b = 0; b < 32; b++) out_slot1[b] = mem[1][b];\n"
        "    for (int b = 0; b < 32; b++) out_slotlast[b] = mem[SHA256MEM_SLOTS-1][b];\n"
        "\n"
        "    /* First mix index */\n"
        "    uint fidx = ((uint)mem[SHA256MEM_SLOTS-1][0]) |\n"
        "                ((uint)mem[SHA256MEM_SLOTS-1][1] << 8) |\n"
        "                ((uint)mem[SHA256MEM_SLOTS-1][2] << 16) |\n"
        "                ((uint)mem[SHA256MEM_SLOTS-1][3] << 24);\n"
        "    fidx %= SHA256MEM_SLOTS;\n"
        "    *out_first_idx = fidx;\n"
        "\n"
        "    /* Phase 3: Mix */\n"
        "    uchar acc[32];\n"
        "    for (int b = 0; b < 32; b++) acc[b] = mem[SHA256MEM_SLOTS-1][b];\n"
        "\n"
        "    for (int i = 0; i < SHA256MEM_MIX_ROUNDS; i++) {\n"
        "        uint idx = ((uint)acc[0]) | ((uint)acc[1] << 8) |\n"
        "                   ((uint)acc[2] << 16) | ((uint)acc[3] << 24);\n"
        "        idx %= SHA256MEM_SLOTS;\n"
        "        uchar buf[64];\n"
        "        for (int b = 0; b < 32; b++) buf[b] = acc[b];\n"
        "        for (int b = 0; b < 32; b++) buf[32+b] = mem[idx][b];\n"
        "        sha256_64(buf, acc);\n"
        "    }\n"
        "\n"
        "    /* Phase 4: Finalize */\n"
        "    uchar final_hash[32];\n"
        "    sha256_32(acc, final_hash);\n"
        "    for (int b = 0; b < 32; b++) out_hash[b] = final_hash[b];\n"
        "}\n";

    /* Concatenate original kernel source + validation kernel */
    size_t vlen = strlen(validation_kernel);
    size_t total_len = src_len + vlen;
    char *full_src = malloc(total_len + 1);
    memcpy(full_src, ksrc, src_len);
    memcpy(full_src + src_len, validation_kernel, vlen);
    full_src[total_len] = '\0';
    free(ksrc);

    cl_program prog = clCreateProgramWithSource(ctx, 1, (const char **)&full_src, &total_len, &err);
    if (err != CL_SUCCESS) { fprintf(stderr, "create program: %d\n", err); return 1; }

    printf("  Compiling validation kernel...\n");
    err = clBuildProgram(prog, 1, &device, "-cl-mad-enable", NULL, NULL);
    if (err != CL_SUCCESS) {
        size_t log_len;
        clGetProgramBuildInfo(prog, device, CL_PROGRAM_BUILD_LOG, 0, NULL, &log_len);
        char *log = malloc(log_len + 1);
        clGetProgramBuildInfo(prog, device, CL_PROGRAM_BUILD_LOG, log_len, log, NULL);
        log[log_len] = '\0';
        fprintf(stderr, "Build failed:\n%s\n", log);
        free(log);
        return 1;
    }
    printf("  Kernel compiled OK.\n\n");
    free(full_src);

    cl_kernel kernel = clCreateKernel(prog, "sha256mem_validate", &err);
    if (err != CL_SUCCESS) { fprintf(stderr, "create kernel: %d\n", err); return 1; }

    /* Allocate GPU buffers */
    size_t mem_size = (size_t)SHA256MEM_SLOTS * 32;
    cl_mem buf_header = clCreateBuffer(ctx, CL_MEM_READ_ONLY | CL_MEM_COPY_HOST_PTR, 80, header, &err);
    CL_CHECK(err, "alloc header");
    cl_mem buf_mem = clCreateBuffer(ctx, CL_MEM_READ_WRITE, mem_size, NULL, &err);
    CL_CHECK(err, "alloc mem pool");
    cl_mem buf_hash = clCreateBuffer(ctx, CL_MEM_WRITE_ONLY, 32, NULL, &err);
    CL_CHECK(err, "alloc hash out");
    cl_mem buf_s0 = clCreateBuffer(ctx, CL_MEM_WRITE_ONLY, 32, NULL, &err);
    CL_CHECK(err, "alloc slot0");
    cl_mem buf_s1 = clCreateBuffer(ctx, CL_MEM_WRITE_ONLY, 32, NULL, &err);
    CL_CHECK(err, "alloc slot1");
    cl_mem buf_sl = clCreateBuffer(ctx, CL_MEM_WRITE_ONLY, 32, NULL, &err);
    CL_CHECK(err, "alloc slotlast");
    uint32_t gpu_first_idx = 0;
    cl_mem buf_fidx = clCreateBuffer(ctx, CL_MEM_WRITE_ONLY, sizeof(uint32_t), NULL, &err);
    CL_CHECK(err, "alloc first_idx");

    CL_CHECK(clSetKernelArg(kernel, 0, sizeof(cl_mem), &buf_header), "arg 0");
    CL_CHECK(clSetKernelArg(kernel, 1, sizeof(cl_mem), &buf_mem), "arg 1");
    CL_CHECK(clSetKernelArg(kernel, 2, sizeof(cl_mem), &buf_hash), "arg 2");
    CL_CHECK(clSetKernelArg(kernel, 3, sizeof(cl_mem), &buf_s0), "arg 3");
    CL_CHECK(clSetKernelArg(kernel, 4, sizeof(cl_mem), &buf_s1), "arg 4");
    CL_CHECK(clSetKernelArg(kernel, 5, sizeof(cl_mem), &buf_sl), "arg 5");
    CL_CHECK(clSetKernelArg(kernel, 6, sizeof(cl_mem), &buf_fidx), "arg 6");

    printf("  Running 1 GPU worker (single hash)...\n");
    size_t global = 1, local = 1;
    CL_CHECK(clEnqueueNDRangeKernel(queue, kernel, 1, NULL, &global, &local, 0, NULL, NULL), "enqueue");
    CL_CHECK(clFinish(queue), "finish");

    /* Read back results */
    uint8_t gpu_hash[32], gpu_s0[32], gpu_s1[32], gpu_sl[32];
    CL_CHECK(clEnqueueReadBuffer(queue, buf_hash, CL_TRUE, 0, 32, gpu_hash, 0, NULL, NULL), "read hash");
    CL_CHECK(clEnqueueReadBuffer(queue, buf_s0, CL_TRUE, 0, 32, gpu_s0, 0, NULL, NULL), "read s0");
    CL_CHECK(clEnqueueReadBuffer(queue, buf_s1, CL_TRUE, 0, 32, gpu_s1, 0, NULL, NULL), "read s1");
    CL_CHECK(clEnqueueReadBuffer(queue, buf_sl, CL_TRUE, 0, 32, gpu_sl, 0, NULL, NULL), "read sl");
    CL_CHECK(clEnqueueReadBuffer(queue, buf_fidx, CL_TRUE, 0, sizeof(uint32_t), &gpu_first_idx, 0, NULL, NULL), "read fidx");

    printf("  Done.\n\n");

    /* ── Compare ───────────────────────────────────────────────────── */
    printf("  ┌─────────────────────────────────────────────────────────────────────────┐\n");
    printf("  │                    PHASE-BY-PHASE COMPARISON                            │\n");
    printf("  ├─────────────────────────────────────────────────────────────────────────┤\n");

    printf("  │ Fill Phase — slot[0] (SHA256 of header):                                │\n");
    print_hex("CPU slot[0]", cpu_hash, 0); /* placeholder, recompute */
    /* Actually recompute for display */
    {
        uint8_t s0_cpu[32];
        SHA256(header, 80, s0_cpu);
        print_hex("  CPU slot[0]   ", s0_cpu, 32);
        print_hex("  GPU slot[0]   ", gpu_s0, 32);
        printf("  %s\n\n", memcmp(s0_cpu, gpu_s0, 32) == 0 ? "✓ MATCH" : "✗ MISMATCH");

        uint8_t s1_cpu[32];
        SHA256(s0_cpu, 32, s1_cpu);
        print_hex("  CPU slot[1]   ", s1_cpu, 32);
        print_hex("  GPU slot[1]   ", gpu_s1, 32);
        printf("  %s\n\n", memcmp(s1_cpu, gpu_s1, 32) == 0 ? "✓ MATCH" : "✗ MISMATCH");
    }

    printf("  │ Fill Phase — slot[%d] (last):                                    │\n", SHA256MEM_SLOTS-1);
    /* We already computed cpu_mem above but freed it. Recompute last slot. */
    {
        uint8_t prev[32], cur[32];
        SHA256(header, 80, prev);
        for (int i = 1; i < SHA256MEM_SLOTS; i++) {
            SHA256(prev, 32, cur);
            memcpy(prev, cur, 32);
        }
        print_hex("  CPU slot[last]", prev, 32);
        print_hex("  GPU slot[last]", gpu_sl, 32);
        printf("  %s\n\n", memcmp(prev, gpu_sl, 32) == 0 ? "✓ MATCH" : "✗ MISMATCH");
    }

    printf("  │ First mix index:                                                        │\n");
    printf("    CPU: %u   GPU: %u   %s\n\n",
           cpu_first_idx, gpu_first_idx,
           cpu_first_idx == gpu_first_idx ? "✓ MATCH" : "✗ MISMATCH");

    printf("  │ Final hash:                                                             │\n");
    print_hex("  CPU final", cpu_hash, 32);
    print_hex("  GPU final", gpu_hash, 32);

    int match = memcmp(cpu_hash, gpu_hash, 32) == 0;
    printf("\n");
    if (match) {
        printf("  ╔═══════════════════════════════════════╗\n");
        printf("  ║  ✓ GPU HASH MATCHES CPU — VALID      ║\n");
        printf("  ╚═══════════════════════════════════════╝\n");
    } else {
        printf("  ╔═══════════════════════════════════════╗\n");
        printf("  ║  ✗ GPU HASH DOES NOT MATCH CPU!      ║\n");
        printf("  ║  GPU is computing INVALID hashes!    ║\n");
        printf("  ╚═══════════════════════════════════════╝\n");

        /* Find first differing byte */
        for (int i = 0; i < 32; i++) {
            if (cpu_hash[i] != gpu_hash[i]) {
                printf("  First difference at byte %d: CPU=0x%02x GPU=0x%02x\n",
                       i, cpu_hash[i], gpu_hash[i]);
                break;
            }
        }
    }
    printf("  └─────────────────────────────────────────────────────────────────────────┘\n\n");

    /* Cleanup */
    clReleaseMemObject(buf_header);
    clReleaseMemObject(buf_mem);
    clReleaseMemObject(buf_hash);
    clReleaseMemObject(buf_s0);
    clReleaseMemObject(buf_s1);
    clReleaseMemObject(buf_sl);
    clReleaseMemObject(buf_fidx);
    clReleaseKernel(kernel);
    clReleaseProgram(prog);
    clReleaseCommandQueue(queue);
    clReleaseContext(ctx);

    return match ? 0 : 1;
}
