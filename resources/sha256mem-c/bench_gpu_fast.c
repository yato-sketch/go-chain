/*
 * sha256mem Optimized GPU Benchmark (OpenCL)
 * ===========================================
 * Benchmarks the optimized sha256mem kernel with midstate pre-computation,
 * uint[8] vector memory operations, and batched nonce dispatch.
 *
 * Build:
 *   gcc -O2 -o sha256mem_gpu_fast bench_gpu_fast.c -lOpenCL -lcrypto -lm
 *
 * Run:
 *   ./sha256mem_gpu_fast                  # auto-detect max workers
 *   ./sha256mem_gpu_fast 64 4             # 64 workers, 4 hashes each
 *   ./sha256mem_gpu_fast 93 10 5          # 93 workers, 10 hashes each, 5 batches
 *
 * Copyright (c) 2024-2026 The Fairchain Contributors
 * Distributed under the MIT software license.
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <time.h>
#include <math.h>
#include <openssl/sha.h>

#ifdef __APPLE__
#include <OpenCL/opencl.h>
#else
#include <CL/cl.h>
#endif

static const char *cl_err_str(cl_int err)
{
    switch (err) {
    case CL_SUCCESS:                        return "CL_SUCCESS";
    case CL_DEVICE_NOT_FOUND:               return "CL_DEVICE_NOT_FOUND";
    case CL_BUILD_PROGRAM_FAILURE:          return "CL_BUILD_PROGRAM_FAILURE";
    case CL_INVALID_VALUE:                  return "CL_INVALID_VALUE";
    case CL_INVALID_KERNEL_NAME:            return "CL_INVALID_KERNEL_NAME";
    case CL_INVALID_WORK_GROUP_SIZE:        return "CL_INVALID_WORK_GROUP_SIZE";
    case CL_MEM_OBJECT_ALLOCATION_FAILURE:  return "CL_MEM_OBJECT_ALLOCATION_FAILURE";
    case CL_OUT_OF_RESOURCES:               return "CL_OUT_OF_RESOURCES";
    case CL_OUT_OF_HOST_MEMORY:             return "CL_OUT_OF_HOST_MEMORY";
    case CL_INVALID_MEM_OBJECT:             return "CL_INVALID_MEM_OBJECT";
    case CL_INVALID_BUFFER_SIZE:            return "CL_INVALID_BUFFER_SIZE";
    default: return "UNKNOWN";
    }
}

#define CL_CHECK(call, msg) do { \
    cl_int _err = (call); \
    if (_err != CL_SUCCESS) { \
        fprintf(stderr, "OpenCL error: %s (%d: %s)\n", msg, _err, cl_err_str(_err)); \
        exit(1); \
    } \
} while(0)

static char *load_kernel_source(const char *path, size_t *len)
{
    FILE *f = fopen(path, "r");
    if (!f) {
        fprintf(stderr, "Cannot open kernel file: %s\n", path);
        exit(1);
    }
    fseek(f, 0, SEEK_END);
    *len = (size_t)ftell(f);
    fseek(f, 0, SEEK_SET);
    char *src = malloc(*len + 1);
    if (fread(src, 1, *len, f) != *len) {
        fprintf(stderr, "Failed to read kernel file\n");
        exit(1);
    }
    src[*len] = '\0';
    fclose(f);
    return src;
}

static inline uint32_t bswap32(uint32_t x)
{
    return ((x & 0xFF) << 24) | ((x & 0xFF00) << 8) |
           ((x >> 8) & 0xFF00) | ((x >> 24) & 0xFF);
}

/*
 * Compute SHA256 midstate: process the first 64 bytes of the 80-byte header.
 * Returns the intermediate SHA256 state (8 x uint32, big-endian internal).
 * Also returns the tail (last 16 bytes) as 4 x uint32 big-endian.
 */
static void compute_midstate(const uint8_t header[80], uint32_t midstate[8], uint32_t tail[4])
{
    /* SHA256 initial state */
    midstate[0] = 0x6a09e667; midstate[1] = 0xbb67ae85;
    midstate[2] = 0x3c6ef372; midstate[3] = 0xa54ff53a;
    midstate[4] = 0x510e527f; midstate[5] = 0x9b05688c;
    midstate[6] = 0x1f83d9ab; midstate[7] = 0x5be0cd19;

    /* Parse first 64 bytes as 16 big-endian uint32s */
    uint32_t W[64];
    for (int i = 0; i < 16; i++)
        W[i] = ((uint32_t)header[i*4] << 24) | ((uint32_t)header[i*4+1] << 16) |
               ((uint32_t)header[i*4+2] << 8) | (uint32_t)header[i*4+3];

    /* Message schedule expansion */
    #define ROTR(x,n) (((x)>>(n))|((x)<<(32-(n))))
    #define s0(x) (ROTR(x,7)^ROTR(x,18)^((x)>>3))
    #define s1(x) (ROTR(x,17)^ROTR(x,19)^((x)>>10))
    #define S0(x) (ROTR(x,2)^ROTR(x,13)^ROTR(x,22))
    #define S1(x) (ROTR(x,6)^ROTR(x,11)^ROTR(x,25))
    #define Ch(x,y,z) (((x)&(y))^(~(x)&(z)))
    #define Maj(x,y,z) (((x)&(y))^((x)&(z))^((y)&(z)))

    for (int i = 16; i < 64; i++)
        W[i] = s1(W[i-2]) + W[i-7] + s0(W[i-15]) + W[i-16];

    static const uint32_t K[64] = {
        0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,
        0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,
        0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,
        0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,
        0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,
        0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,
        0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,
        0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,
        0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,
        0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,
        0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,
        0xd192e819,0xd6990624,0xf40e3585,0x106aa070,
        0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,
        0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,
        0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,
        0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2
    };

    uint32_t a = midstate[0], b = midstate[1], c = midstate[2], d = midstate[3];
    uint32_t e = midstate[4], f = midstate[5], g = midstate[6], h = midstate[7];

    for (int i = 0; i < 64; i++) {
        uint32_t T1 = h + S1(e) + Ch(e,f,g) + K[i] + W[i];
        uint32_t T2 = S0(a) + Maj(a,b,c);
        h = g; g = f; f = e; e = d + T1;
        d = c; c = b; b = a; a = T1 + T2;
    }

    midstate[0] += a; midstate[1] += b; midstate[2] += c; midstate[3] += d;
    midstate[4] += e; midstate[5] += f; midstate[6] += g; midstate[7] += h;

    #undef ROTR
    #undef s0
    #undef s1
    #undef S0
    #undef S1
    #undef Ch
    #undef Maj

    /* Tail: last 16 bytes as big-endian uint32s */
    for (int i = 0; i < 4; i++)
        tail[i] = ((uint32_t)header[64+i*4] << 24) | ((uint32_t)header[64+i*4+1] << 16) |
                  ((uint32_t)header[64+i*4+2] << 8) | (uint32_t)header[64+i*4+3];
}

/*
 * CPU reference hash for validation.
 */
static void sha256mem_cpu(const uint8_t *data, size_t len, uint8_t out[32])
{
    #define SHA256MEM_SLOTS       4194304
    #define SHA256MEM_FILL_CHAINS 8192
    #define SHA256MEM_MIX_ROUNDS  2048

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

    #undef SHA256MEM_SLOTS
    #undef SHA256MEM_FILL_CHAINS
    #undef SHA256MEM_MIX_ROUNDS
}

int main(int argc, char **argv)
{
    const size_t MEM_PER_WORKER = 4194304UL * 32;

    int num_workers = 0;
    int hashes_per_item = 2;
    int num_batches = 3;

    if (argc > 1) num_workers = atoi(argv[1]);
    if (argc > 2) hashes_per_item = atoi(argv[2]);
    if (argc > 3) num_batches = atoi(argv[3]);
    if (hashes_per_item < 1) hashes_per_item = 1;
    if (num_batches < 1) num_batches = 1;

    /* ── OpenCL setup ─────────────────────────────────────────────── */
    cl_platform_id platform;
    cl_device_id device;
    cl_int err;

    CL_CHECK(clGetPlatformIDs(1, &platform, NULL), "get platform");
    CL_CHECK(clGetDeviceIDs(platform, CL_DEVICE_TYPE_GPU, 1, &device, NULL), "get device");

    char dev_name[256];
    size_t dev_gmem;
    cl_uint dev_cu, dev_freq;
    clGetDeviceInfo(device, CL_DEVICE_NAME, sizeof(dev_name), dev_name, NULL);
    clGetDeviceInfo(device, CL_DEVICE_GLOBAL_MEM_SIZE, sizeof(dev_gmem), &dev_gmem, NULL);
    clGetDeviceInfo(device, CL_DEVICE_MAX_COMPUTE_UNITS, sizeof(dev_cu), &dev_cu, NULL);
    clGetDeviceInfo(device, CL_DEVICE_MAX_CLOCK_FREQUENCY, sizeof(dev_freq), &dev_freq, NULL);

    /* Auto-detect max workers if not specified */
    if (num_workers <= 0) {
        num_workers = (int)((dev_gmem * 0.85) / MEM_PER_WORKER);
        if (num_workers < 1) num_workers = 1;
    }

    size_t total_vram = (size_t)num_workers * MEM_PER_WORKER;

    printf("╔══════════════════════════════════════════════════════════════╗\n");
    printf("║     sha256mem OPTIMIZED GPU Benchmark — Fairchain PoW      ║\n");
    printf("╠══════════════════════════════════════════════════════════════╣\n");
    printf("║  GPU:          %-42s║\n", dev_name);
    printf("║  Compute units: %-3u @ %u MHz                              ║\n", dev_cu, dev_freq);
    printf("║  VRAM:          %lu MiB total, %lu MiB used               ║\n",
           (unsigned long)(dev_gmem / (1024*1024)),
           (unsigned long)(total_vram / (1024*1024)));
    printf("║  Workers:       %-4d  (128 MiB each)                      ║\n", num_workers);
    printf("║  Hashes/worker: %-4d                                      ║\n", hashes_per_item);
    printf("║  Batches:       %-4d                                      ║\n", num_batches);
    printf("║  Optimizations: midstate, uint[8] vectors, register SHA256 ║\n");
    printf("╚══════════════════════════════════════════════════════════════╝\n\n");

    if (total_vram > (size_t)(dev_gmem * 0.95)) {
        fprintf(stderr, "ERROR: Not enough VRAM. Need %lu MiB, have %lu MiB.\n",
                (unsigned long)(total_vram / (1024*1024)),
                (unsigned long)(dev_gmem / (1024*1024)));
        return 1;
    }

    cl_context ctx = clCreateContext(NULL, 1, &device, NULL, NULL, &err);
    if (err != CL_SUCCESS) { fprintf(stderr, "create context: %d\n", err); return 1; }

    cl_command_queue queue = clCreateCommandQueue(ctx, device, 0, &err);
    if (err != CL_SUCCESS) { fprintf(stderr, "create queue: %d\n", err); return 1; }

    /* Load and build kernel */
    size_t src_len;
    char *src = load_kernel_source("sha256mem_gpu_fast.cl", &src_len);
    cl_program prog = clCreateProgramWithSource(ctx, 1, (const char **)&src, &src_len, &err);
    if (err != CL_SUCCESS) { fprintf(stderr, "create program: %d\n", err); return 1; }

    printf("  Compiling optimized kernel...\n");
    err = clBuildProgram(prog, 1, &device, "-cl-mad-enable -cl-fast-relaxed-math -cl-std=CL1.2", NULL, NULL);
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
    free(src);

    /* ── Prepare header and midstate ──────────────────────────────── */
    uint8_t header[80];
    memset(header, 0, sizeof(header));
    header[0] = 0x01; /* version */

    uint32_t midstate[8], tail[4];
    compute_midstate(header, midstate, tail);

    printf("  Midstate pre-computed on CPU (saves 1 SHA256 block per hash).\n");

    /* ── First: validate GPU matches CPU ──────────────────────────── */
    printf("  Validating GPU correctness against CPU reference...\n");
    {
        uint8_t cpu_hash[32];
        sha256mem_cpu(header, 80, cpu_hash);

        cl_kernel vkernel = clCreateKernel(prog, "sha256mem_validate_fast", &err);
        if (err != CL_SUCCESS) { fprintf(stderr, "create validate kernel: %d (%s)\n", err, cl_err_str(err)); return 1; }

        cl_mem buf_ms = clCreateBuffer(ctx, CL_MEM_READ_ONLY | CL_MEM_COPY_HOST_PTR,
                                       8 * sizeof(uint32_t), midstate, &err);
        CL_CHECK(err, "alloc midstate");
        cl_mem buf_tail = clCreateBuffer(ctx, CL_MEM_READ_ONLY | CL_MEM_COPY_HOST_PTR,
                                         4 * sizeof(uint32_t), tail, &err);
        CL_CHECK(err, "alloc tail");
        cl_mem buf_vmem = clCreateBuffer(ctx, CL_MEM_READ_WRITE, MEM_PER_WORKER, NULL, &err);
        CL_CHECK(err, "alloc validate mem");
        cl_mem buf_vhash = clCreateBuffer(ctx, CL_MEM_WRITE_ONLY, 32, NULL, &err);
        CL_CHECK(err, "alloc validate hash");
        cl_mem buf_vs0 = clCreateBuffer(ctx, CL_MEM_WRITE_ONLY, 32, NULL, &err);
        CL_CHECK(err, "alloc validate s0");
        cl_mem buf_vsl = clCreateBuffer(ctx, CL_MEM_WRITE_ONLY, 32, NULL, &err);
        CL_CHECK(err, "alloc validate slast");

        CL_CHECK(clSetKernelArg(vkernel, 0, sizeof(cl_mem), &buf_ms), "varg 0");
        CL_CHECK(clSetKernelArg(vkernel, 1, sizeof(cl_mem), &buf_tail), "varg 1");
        CL_CHECK(clSetKernelArg(vkernel, 2, sizeof(cl_mem), &buf_vmem), "varg 2");
        CL_CHECK(clSetKernelArg(vkernel, 3, sizeof(cl_mem), &buf_vhash), "varg 3");
        CL_CHECK(clSetKernelArg(vkernel, 4, sizeof(cl_mem), &buf_vs0), "varg 4");
        CL_CHECK(clSetKernelArg(vkernel, 5, sizeof(cl_mem), &buf_vsl), "varg 5");

        size_t g = 1, l = 1;
        CL_CHECK(clEnqueueNDRangeKernel(queue, vkernel, 1, NULL, &g, &l, 0, NULL, NULL), "validate enqueue");
        CL_CHECK(clFinish(queue), "validate finish");

        uint8_t gpu_hash[32];
        CL_CHECK(clEnqueueReadBuffer(queue, buf_vhash, CL_TRUE, 0, 32, gpu_hash, 0, NULL, NULL), "read vhash");

        int match = memcmp(cpu_hash, gpu_hash, 32) == 0;
        printf("  CPU: ");
        for (int i = 0; i < 32; i++) printf("%02x", cpu_hash[i]);
        printf("\n  GPU: ");
        for (int i = 0; i < 32; i++) printf("%02x", gpu_hash[i]);
        printf("\n  %s\n\n", match ? "MATCH — GPU kernel is correct." : "MISMATCH — GPU kernel has a bug!");

        if (!match) {
            fprintf(stderr, "ABORTING: GPU hash does not match CPU reference.\n");
            return 1;
        }

        clReleaseMemObject(buf_ms);
        clReleaseMemObject(buf_tail);
        clReleaseMemObject(buf_vmem);
        clReleaseMemObject(buf_vhash);
        clReleaseMemObject(buf_vs0);
        clReleaseMemObject(buf_vsl);
        clReleaseKernel(vkernel);
    }

    /* ── Allocate benchmark buffers ───────────────────────────────── */
    cl_mem buf_midstate = clCreateBuffer(ctx, CL_MEM_READ_ONLY | CL_MEM_COPY_HOST_PTR,
                                         8 * sizeof(uint32_t), midstate, &err);
    CL_CHECK(err, "alloc midstate");

    cl_mem buf_tail = clCreateBuffer(ctx, CL_MEM_READ_ONLY | CL_MEM_COPY_HOST_PTR,
                                     4 * sizeof(uint32_t), tail, &err);
    CL_CHECK(err, "alloc tail");

    printf("  Allocating %lu MiB VRAM for %d workers...\n",
           (unsigned long)(total_vram / (1024*1024)), num_workers);
    cl_mem buf_mem = clCreateBuffer(ctx, CL_MEM_READ_WRITE, total_vram, NULL, &err);
    if (err != CL_SUCCESS) {
        fprintf(stderr, "VRAM allocation failed: %d (%s)\n", err, cl_err_str(err));
        return 1;
    }

    uint32_t *hash_counts_host = calloc(num_workers, sizeof(uint32_t));
    cl_mem buf_counts = clCreateBuffer(ctx, CL_MEM_READ_WRITE,
                                       num_workers * sizeof(uint32_t), NULL, &err);
    CL_CHECK(err, "alloc counts");

    uint32_t found_flag = 0;
    cl_mem buf_found = clCreateBuffer(ctx, CL_MEM_READ_WRITE | CL_MEM_COPY_HOST_PTR,
                                      sizeof(uint32_t), &found_flag, &err);
    CL_CHECK(err, "alloc found flag");

    uint32_t found_nonce = 0;
    cl_mem buf_nonce = clCreateBuffer(ctx, CL_MEM_READ_WRITE | CL_MEM_COPY_HOST_PTR,
                                      sizeof(uint32_t), &found_nonce, &err);
    CL_CHECK(err, "alloc found nonce");

    uint32_t found_hash[8] = {0};
    cl_mem buf_hash = clCreateBuffer(ctx, CL_MEM_READ_WRITE | CL_MEM_COPY_HOST_PTR,
                                     8 * sizeof(uint32_t), found_hash, &err);
    CL_CHECK(err, "alloc found hash");

    /* Impossible target for benchmarking (reject everything, forces all hashes) */
    uint32_t target[8];
    memset(target, 0x00, sizeof(target));
    cl_mem buf_target = clCreateBuffer(ctx, CL_MEM_READ_ONLY | CL_MEM_COPY_HOST_PTR,
                                       8 * sizeof(uint32_t), target, &err);
    CL_CHECK(err, "alloc target");

    cl_kernel kernel = clCreateKernel(prog, "sha256mem_mine_fast", &err);
    if (err != CL_SUCCESS) { fprintf(stderr, "create kernel: %d (%s)\n", err, cl_err_str(err)); return 1; }

    CL_CHECK(clSetKernelArg(kernel, 0, sizeof(cl_mem), &buf_midstate), "arg 0");
    CL_CHECK(clSetKernelArg(kernel, 1, sizeof(cl_mem), &buf_tail), "arg 1");
    CL_CHECK(clSetKernelArg(kernel, 2, sizeof(cl_mem), &buf_mem), "arg 2");
    CL_CHECK(clSetKernelArg(kernel, 3, sizeof(cl_mem), &buf_counts), "arg 3");
    CL_CHECK(clSetKernelArg(kernel, 4, sizeof(cl_mem), &buf_found), "arg 4");
    CL_CHECK(clSetKernelArg(kernel, 5, sizeof(cl_mem), &buf_nonce), "arg 5");
    CL_CHECK(clSetKernelArg(kernel, 6, sizeof(cl_mem), &buf_hash), "arg 6");
    CL_CHECK(clSetKernelArg(kernel, 7, sizeof(cl_mem), &buf_target), "arg 7");

    uint32_t hpi = (uint32_t)hashes_per_item;
    CL_CHECK(clSetKernelArg(kernel, 9, sizeof(uint32_t), &hpi), "arg 9");

    printf("  VRAM allocated OK.\n\n");

    /* ── Benchmark: run multiple batches ──────────────────────────── */
    size_t global_size = (size_t)num_workers;
    size_t local_size = 1;

    printf("  Running %d batches of %d workers x %d hashes...\n",
           num_batches, num_workers, hashes_per_item);
    printf("  (Each batch synchronized individually for accurate timing)\n\n");

    struct timespec ts_start, ts_end;
    uint64_t total_hashes = 0;
    double total_elapsed = 0.0;

    /* Warm-up run (not timed) to prime GPU caches and scheduler */
    {
        uint32_t nonce_start = 0xF0000000;
        CL_CHECK(clSetKernelArg(kernel, 8, sizeof(uint32_t), &nonce_start), "warmup arg 8");
        CL_CHECK(clEnqueueNDRangeKernel(queue, kernel, 1, NULL,
                                         &global_size, &local_size, 0, NULL, NULL),
                 "warmup enqueue");
        CL_CHECK(clFinish(queue), "warmup finish");
        printf("  Warm-up batch complete.\n");
    }

    clock_gettime(CLOCK_MONOTONIC, &ts_start);

    for (int batch = 0; batch < num_batches; batch++) {
        uint32_t nonce_start = (uint32_t)(batch * num_workers * hashes_per_item);
        CL_CHECK(clSetKernelArg(kernel, 8, sizeof(uint32_t), &nonce_start), "arg 8 nonce");

        /* Reset counts */
        memset(hash_counts_host, 0, num_workers * sizeof(uint32_t));
        CL_CHECK(clEnqueueWriteBuffer(queue, buf_counts, CL_TRUE, 0,
                                       num_workers * sizeof(uint32_t), hash_counts_host,
                                       0, NULL, NULL), "reset counts");

        /* Reset found flag */
        found_flag = 0;
        CL_CHECK(clEnqueueWriteBuffer(queue, buf_found, CL_TRUE, 0,
                                       sizeof(uint32_t), &found_flag,
                                       0, NULL, NULL), "reset found");

        struct timespec batch_start, batch_end;
        clock_gettime(CLOCK_MONOTONIC, &batch_start);

        CL_CHECK(clEnqueueNDRangeKernel(queue, kernel, 1, NULL,
                                         &global_size, &local_size, 0, NULL, NULL),
                 "enqueue kernel");
        CL_CHECK(clFinish(queue), "finish batch");

        clock_gettime(CLOCK_MONOTONIC, &batch_end);

        double batch_elapsed = (batch_end.tv_sec - batch_start.tv_sec)
                             + (batch_end.tv_nsec - batch_start.tv_nsec) / 1e9;

        /* Read actual hash counts */
        CL_CHECK(clEnqueueReadBuffer(queue, buf_counts, CL_TRUE, 0,
                                      num_workers * sizeof(uint32_t), hash_counts_host,
                                      0, NULL, NULL), "read counts");

        uint64_t batch_hashes = 0;
        for (int i = 0; i < num_workers; i++)
            batch_hashes += hash_counts_host[i];

        total_hashes += batch_hashes;

        double batch_rate = (double)batch_hashes / batch_elapsed;
        printf("  Batch %2d: %lu hashes in %.3fs = %.1f H/s\n",
               batch + 1, (unsigned long)batch_hashes, batch_elapsed, batch_rate);
    }

    clock_gettime(CLOCK_MONOTONIC, &ts_end);
    total_elapsed = (ts_end.tv_sec - ts_start.tv_sec)
                  + (ts_end.tv_nsec - ts_start.tv_nsec) / 1e9;
    double rate = (double)total_hashes / total_elapsed;
    double elapsed = total_elapsed;
    printf("\n");

    /* ── Results ──────────────────────────────────────────────────── */
    printf("╔══════════════════════════════════════════════════════════════╗\n");
    printf("║              OPTIMIZED GPU BENCHMARK RESULTS               ║\n");
    printf("╠══════════════════════════════════════════════════════════════╣\n");
    printf("║  GPU:            %-40s  ║\n", dev_name);
    printf("║  Compute units:  %-3u @ %u MHz                             ║\n", dev_cu, dev_freq);
    printf("║  Workers:        %-4d (128 MiB each)                      ║\n", num_workers);
    printf("║  VRAM used:      %lu MiB                                  ║\n",
           (unsigned long)(total_vram / (1024*1024)));
    printf("║  Batches:        %-4d                                      ║\n", num_batches);
    printf("║                                                            ║\n");
    printf("║  Total hashes:   %-10lu                                  ║\n", (unsigned long)total_hashes);
    printf("║  Wall time:      %.3f seconds                             ║\n", elapsed);
    printf("║  Hashrate:       %.2f H/s                                 ║\n", rate);
    printf("║  Per worker:     %.3f H/s                                 ║\n", rate / num_workers);
    printf("║  Per SM:         %.3f H/s                                 ║\n", rate / dev_cu);
    printf("╚══════════════════════════════════════════════════════════════╝\n\n");

    /* Comparison */
    printf("  ┌─────────────────────────────────────────────────────────┐\n");
    printf("  │              COMPARISON: Optimized GPU vs CPU            │\n");
    printf("  ├─────────────────────────────────────────────────────────┤\n");
    printf("  │  This GPU (%d workers):     %8.2f H/s               │\n", num_workers, rate);
    printf("  │  Old GPU kernel (naive):          ~24 H/s               │\n");
    printf("  │  i9-11900K (1 thread):             27 H/s               │\n");
    printf("  │  i9-11900K (16 threads):           85 H/s               │\n");
    printf("  │  Galaxy S10+ (1 thread):           28 H/s               │\n");
    printf("  │                                                         │\n");
    if (rate > 0) {
        printf("  │  Speedup vs naive GPU:       %.1fx                     │\n", rate / 24.0);
        printf("  │  Speedup vs single CPU core: %.1fx                     │\n", rate / 27.0);
        printf("  │  Speedup vs 16-thread CPU:   %.1fx                     │\n", rate / 85.0);
    }
    printf("  └─────────────────────────────────────────────────────────┘\n\n");

    /* Cleanup */
    clReleaseMemObject(buf_midstate);
    clReleaseMemObject(buf_tail);
    clReleaseMemObject(buf_mem);
    clReleaseMemObject(buf_counts);
    clReleaseMemObject(buf_found);
    clReleaseMemObject(buf_nonce);
    clReleaseMemObject(buf_hash);
    clReleaseMemObject(buf_target);
    clReleaseKernel(kernel);
    clReleaseProgram(prog);
    clReleaseCommandQueue(queue);
    clReleaseContext(ctx);
    free(hash_counts_host);

    return 0;
}
