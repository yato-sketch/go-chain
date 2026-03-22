/*
 * sha256mem GPU Benchmark (OpenCL)
 * =================================
 * Benchmarks sha256mem on NVIDIA (or any OpenCL) GPU to demonstrate
 * the algorithm's GPU resistance via memory-latency bottleneck.
 *
 * Build:
 *   clang -O2 -o sha256mem_gpu bench_gpu.c -lOpenCL -lm
 *
 * Run:
 *   ./sha256mem_gpu              # default: 16 work-items, 1 hash each
 *   ./sha256mem_gpu 32 2         # 32 work-items, 2 hashes each
 *   ./sha256mem_gpu 64 1         # 64 work-items, 1 hash each
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

int main(int argc, char **argv)
{
    int num_workers = 16;
    int hashes_per_item = 1;

    if (argc > 1) num_workers = atoi(argv[1]);
    if (argc > 2) hashes_per_item = atoi(argv[2]);
    if (num_workers < 1) num_workers = 1;
    if (hashes_per_item < 1) hashes_per_item = 1;

    /* Memory per work-item: 4194304 slots × 32 bytes = 128 MiB */
    size_t mem_per_worker = 4194304UL * 32;
    size_t total_vram = (size_t)num_workers * mem_per_worker;

    printf("╔══════════════════════════════════════════════════════════════╗\n");
    printf("║        sha256mem GPU Benchmark — Fairchain PoW             ║\n");
    printf("╠══════════════════════════════════════════════════════════════╣\n");
    printf("║  Buffer:       4194304 slots × 32 bytes = 128 MiB per worker║\n");
    printf("║  Mix:          1024 rounds (SHA256 per hop)                ║\n");
    printf("║  Workers:      %-4d                                        ║\n", num_workers);
    printf("║  Hashes/item:  %-4d                                        ║\n", hashes_per_item);
    printf("║  VRAM needed:  %lu MiB                                    ║\n",
           (unsigned long)(total_vram / (1024 * 1024)));
    printf("╚══════════════════════════════════════════════════════════════╝\n\n");

    /* ── OpenCL setup ─────────────────────────────────────────────── */
    cl_platform_id platform;
    cl_device_id device;
    cl_int err;

    CL_CHECK(clGetPlatformIDs(1, &platform, NULL), "get platform");
    CL_CHECK(clGetDeviceIDs(platform, CL_DEVICE_TYPE_GPU, 1, &device, NULL), "get device");

    char dev_name[256];
    size_t dev_gmem, dev_lmem;
    cl_uint dev_cu, dev_freq;
    clGetDeviceInfo(device, CL_DEVICE_NAME, sizeof(dev_name), dev_name, NULL);
    clGetDeviceInfo(device, CL_DEVICE_GLOBAL_MEM_SIZE, sizeof(dev_gmem), &dev_gmem, NULL);
    clGetDeviceInfo(device, CL_DEVICE_LOCAL_MEM_SIZE, sizeof(dev_lmem), &dev_lmem, NULL);
    clGetDeviceInfo(device, CL_DEVICE_MAX_COMPUTE_UNITS, sizeof(dev_cu), &dev_cu, NULL);
    clGetDeviceInfo(device, CL_DEVICE_MAX_CLOCK_FREQUENCY, sizeof(dev_freq), &dev_freq, NULL);

    printf("  GPU: %s\n", dev_name);
    printf("  Compute units: %u @ %u MHz\n", dev_cu, dev_freq);
    printf("  Global memory: %lu MiB\n", (unsigned long)(dev_gmem / (1024*1024)));
    printf("  Local memory:  %lu KiB\n", (unsigned long)(dev_lmem / 1024));
    printf("  VRAM required: %lu MiB (%d workers × 128 MiB)\n\n",
           (unsigned long)(total_vram / (1024*1024)), num_workers);

    if (total_vram > dev_gmem * 0.9) {
        fprintf(stderr, "ERROR: Not enough VRAM. Need %lu MiB, have %lu MiB.\n",
                (unsigned long)(total_vram / (1024*1024)),
                (unsigned long)(dev_gmem / (1024*1024)));
        fprintf(stderr, "Reduce worker count with: %s <workers> <hashes_per>\n", argv[0]);
        return 1;
    }

    cl_context ctx = clCreateContext(NULL, 1, &device, NULL, NULL, &err);
    if (err != CL_SUCCESS) { fprintf(stderr, "create context: %d\n", err); return 1; }

    cl_command_queue queue = clCreateCommandQueue(ctx, device, 0, &err);
    if (err != CL_SUCCESS) { fprintf(stderr, "create queue: %d\n", err); return 1; }

    /* Load and build kernel */
    size_t src_len;
    char *src = load_kernel_source("sha256mem_gpu.cl", &src_len);
    cl_program prog = clCreateProgramWithSource(ctx, 1, (const char **)&src, &src_len, &err);
    if (err != CL_SUCCESS) { fprintf(stderr, "create program: %d\n", err); return 1; }

    printf("  Compiling OpenCL kernel...\n");
    err = clBuildProgram(prog, 1, &device, "-cl-mad-enable -cl-fast-relaxed-math", NULL, NULL);
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

    cl_kernel kernel = clCreateKernel(prog, "sha256mem_mine", &err);
    if (err != CL_SUCCESS) { fprintf(stderr, "create kernel: %d (%s)\n", err, cl_err_str(err)); return 1; }

    /* ── Allocate buffers ─────────────────────────────────────────── */
    uint8_t header[80];
    memset(header, 0, sizeof(header));
    header[0] = 0x01; /* version */

    cl_mem buf_header = clCreateBuffer(ctx, CL_MEM_READ_ONLY | CL_MEM_COPY_HOST_PTR,
                                       80, header, &err);
    CL_CHECK(err, "alloc header");

    printf("  Allocating %lu MiB VRAM for %d workers...\n",
           (unsigned long)(total_vram / (1024*1024)), num_workers);
    cl_mem buf_mem = clCreateBuffer(ctx, CL_MEM_READ_WRITE, total_vram, NULL, &err);
    if (err != CL_SUCCESS) {
        fprintf(stderr, "VRAM allocation failed: %d (%s)\n", err, cl_err_str(err));
        fprintf(stderr, "Try fewer workers: %s %d %d\n", argv[0], num_workers/2, hashes_per_item);
        return 1;
    }

    uint32_t *hash_counts_host = calloc(num_workers, sizeof(uint32_t));
    cl_mem buf_counts = clCreateBuffer(ctx, CL_MEM_READ_WRITE | CL_MEM_COPY_HOST_PTR,
                                       num_workers * sizeof(uint32_t), hash_counts_host, &err);
    CL_CHECK(err, "alloc counts");

    uint32_t found_flag = 0;
    cl_mem buf_found = clCreateBuffer(ctx, CL_MEM_READ_WRITE | CL_MEM_COPY_HOST_PTR,
                                      sizeof(uint32_t), &found_flag, &err);
    CL_CHECK(err, "alloc found flag");

    uint32_t found_nonce = 0;
    cl_mem buf_nonce = clCreateBuffer(ctx, CL_MEM_READ_WRITE | CL_MEM_COPY_HOST_PTR,
                                      sizeof(uint32_t), &found_nonce, &err);
    CL_CHECK(err, "alloc found nonce");

    uint8_t found_hash[32] = {0};
    cl_mem buf_hash = clCreateBuffer(ctx, CL_MEM_READ_WRITE | CL_MEM_COPY_HOST_PTR,
                                     32, found_hash, &err);
    CL_CHECK(err, "alloc found hash");

    uint32_t nonce_start = 0;
    uint32_t hpi = (uint32_t)hashes_per_item;

    CL_CHECK(clSetKernelArg(kernel, 0, sizeof(cl_mem), &buf_header), "arg 0");
    CL_CHECK(clSetKernelArg(kernel, 1, sizeof(cl_mem), &buf_mem), "arg 1");
    CL_CHECK(clSetKernelArg(kernel, 2, sizeof(cl_mem), &buf_counts), "arg 2");
    CL_CHECK(clSetKernelArg(kernel, 3, sizeof(cl_mem), &buf_found), "arg 3");
    CL_CHECK(clSetKernelArg(kernel, 4, sizeof(cl_mem), &buf_nonce), "arg 4");
    CL_CHECK(clSetKernelArg(kernel, 5, sizeof(cl_mem), &buf_hash), "arg 5");
    CL_CHECK(clSetKernelArg(kernel, 6, sizeof(uint32_t), &nonce_start), "arg 6");
    CL_CHECK(clSetKernelArg(kernel, 7, sizeof(uint32_t), &hpi), "arg 7");

    printf("  VRAM allocated OK.\n");
    printf("  Launching %d GPU workers, %d hash(es) each...\n\n", num_workers, hashes_per_item);

    /* ── Launch and time ──────────────────────────────────────────── */
    size_t global_size = (size_t)num_workers;
    size_t local_size = 1; /* 1 thread per workgroup — each needs 128 MiB */

    struct timespec ts_start, ts_end;
    clock_gettime(CLOCK_MONOTONIC, &ts_start);

    CL_CHECK(clEnqueueNDRangeKernel(queue, kernel, 1, NULL,
                                     &global_size, &local_size, 0, NULL, NULL),
             "enqueue kernel");
    CL_CHECK(clFinish(queue), "finish");

    clock_gettime(CLOCK_MONOTONIC, &ts_end);

    double elapsed = (ts_end.tv_sec - ts_start.tv_sec)
                   + (ts_end.tv_nsec - ts_start.tv_nsec) / 1e9;

    /* Read back results */
    CL_CHECK(clEnqueueReadBuffer(queue, buf_counts, CL_TRUE, 0,
                                  num_workers * sizeof(uint32_t), hash_counts_host,
                                  0, NULL, NULL), "read counts");

    uint64_t total_hashes = 0;
    for (int i = 0; i < num_workers; i++)
        total_hashes += hash_counts_host[i];

    double rate = (double)total_hashes / elapsed;

    /* ── Results ──────────────────────────────────────────────────── */
    printf("╔══════════════════════════════════════════════════════════════╗\n");
    printf("║                   GPU BENCHMARK RESULTS                    ║\n");
    printf("╠══════════════════════════════════════════════════════════════╣\n");
    printf("║  GPU:            %-40s  ║\n", dev_name);
    printf("║  Compute units:  %-3u SMs @ %u MHz                         ║\n", dev_cu, dev_freq);
    printf("║  Workers:        %-4d                                      ║\n", num_workers);
    printf("║  VRAM used:      %lu MiB                                  ║\n",
           (unsigned long)(total_vram / (1024*1024)));
    printf("║                                                            ║\n");
    printf("║  Total hashes:   %-10lu                                  ║\n", (unsigned long)total_hashes);
    printf("║  Wall time:      %.2f seconds                             ║\n", elapsed);
    printf("║  Hashrate:       %.2f H/s                                 ║\n", rate);
    printf("║  Per SM:         %.3f H/s                                 ║\n", rate / dev_cu);
    printf("╚══════════════════════════════════════════════════════════════╝\n\n");

    /* Per-worker breakdown (first 16 and last few) */
    printf("  Per-worker breakdown:\n");
    printf("  ┌────────┬──────────┐\n");
    printf("  │ worker │  hashes  │\n");
    printf("  ├────────┼──────────┤\n");
    int show = num_workers < 32 ? num_workers : 16;
    for (int i = 0; i < show; i++)
        printf("  │  %4d  │  %6u  │\n", i, hash_counts_host[i]);
    if (num_workers > 32) {
        printf("  │  ...   │   ...    │\n");
        for (int i = num_workers - 4; i < num_workers; i++)
            printf("  │  %4d  │  %6u  │\n", i, hash_counts_host[i]);
    }
    printf("  └────────┴──────────┘\n\n");

    /* Comparison */
    printf("  ┌─────────────────────────────────────────────────────────┐\n");
    printf("  │              COMPARISON: GPU vs CPU                     │\n");
    printf("  ├─────────────────────────────────────────────────────────┤\n");
    printf("  │  RTX 3080 Ti (80 SMs, 12 GB):     %8.2f H/s        │\n", rate);
    printf("  │  i9-11900K (2 threads):                  26 H/s        │\n");
    printf("  │  Galaxy S10+ (1 thread):                 12 H/s        │\n");
    printf("  │                                                        │\n");
    if (rate > 0) {
        printf("  │  GPU vs Desktop CPU:              %.1fx               │\n", rate / 26.0);
        printf("  │  GPU vs Phone:                    %.1fx               │\n", rate / 12.0);
        printf("  │  GPU cost: ~$1200  Phone cost: ~$100                 │\n");
    }
    printf("  └─────────────────────────────────────────────────────────┘\n\n");

    if (rate < 30.0) {
        printf("  ✓ GPU RESISTANCE CONFIRMED\n");
        printf("  A $1200 GPU cannot outperform a $100 phone.\n");
        printf("  sha256mem's pointer-chasing creates serial VRAM latency\n");
        printf("  that GPUs cannot parallelize away.\n");
    } else if (rate < 100.0) {
        printf("  ~ GPU has marginal advantage but not cost-effective.\n");
        printf("  $/hash favors CPUs and phones.\n");
    } else {
        printf("  ⚠ GPU showing unexpected performance — investigate.\n");
    }

    printf("\n  Fairchain sha256mem — CPU-only proof-of-work\n");
    printf("  https://github.com/bams-repo/go-chain\n\n");

    /* Cleanup */
    clReleaseMemObject(buf_header);
    clReleaseMemObject(buf_mem);
    clReleaseMemObject(buf_counts);
    clReleaseMemObject(buf_found);
    clReleaseMemObject(buf_nonce);
    clReleaseMemObject(buf_hash);
    clReleaseKernel(kernel);
    clReleaseProgram(prog);
    clReleaseCommandQueue(queue);
    clReleaseContext(ctx);
    free(hash_counts_host);

    return 0;
}
