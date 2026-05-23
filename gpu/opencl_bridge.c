//go:build opencl

/*
 * OpenCL bridge — manages GPU context, memory, and kernel launches
 * for yespower 1.0 mining.
 *
 * Compiled by CGO alongside the Go package.
 */

#include "bridge.h"
#include <CL/cl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/* -------------------------------------------------------------------- */
/*  Constants (must match yespower_kernel.cl)                           */
/* -------------------------------------------------------------------- */
#define B_SIZE     (128 * 32)     /* 4096 */
#define V_SIZE     (B_SIZE * 2048)/* 8388608 */
#define XY_SIZE    (B_SIZE + 64)  /* 4160 */
#define Sbytes     98304
#define PER_THREAD (B_SIZE + V_SIZE + XY_SIZE + Sbytes)

/* -------------------------------------------------------------------- */
/*  Global state                                                        */
/* -------------------------------------------------------------------- */
static cl_platform_id platform;
static cl_device_id  device;
static cl_context    context;
static cl_command_queue queue;
static cl_program   program;
static cl_kernel    kernel;
static int          initialized = 0;
int gpu_max_batch = 0;

static cl_mem d_scratch = NULL;
static cl_mem d_headers = NULL;
static cl_mem d_outputs = NULL;
static cl_mem d_pers    = NULL;
static int    pers_cap  = 0;

/* -------------------------------------------------------------------- */
/*  Kernel source (embedded)                                            */
/* -------------------------------------------------------------------- */
/* yespower_kernel.cl is compiled at runtime via clCreateProgramWithSource.
 * We embed the source as a string for portability; alternatively, load
 * from file.  The source is in yespower_kernel.cl.
 *
 * For the embedded version, keep the source in sync.
 */
static const char *kernel_source = 0; /* loaded from file or embedded */

static int load_kernel_source(void) {
    /* Try to load from file first (for development) */
    FILE *f = fopen("gpu/yespower_kernel.cl", "rb");
    if (!f) f = fopen("yespower_kernel.cl", "rb");
    if (f) {
        fseek(f, 0, SEEK_END);
        long sz = ftell(f);
        fseek(f, 0, SEEK_SET);
        kernel_source = (const char *)malloc(sz + 1);
        if (kernel_source) {
            fread((void *)kernel_source, 1, sz, f);
            ((char *)kernel_source)[sz] = '\0';
        }
        fclose(f);
        return kernel_source ? 0 : -1;
    }
    return -1; /* source not found */
}

/* -------------------------------------------------------------------- */
/*  Public API                                                          */
/* -------------------------------------------------------------------- */

int gpu_init(void) {
    cl_int err;

    /* Get platform */
    cl_uint num_platforms;
    err = clGetPlatformIDs(1, &platform, &num_platforms);
    if (err != CL_SUCCESS || num_platforms < 1) return -1;

    /* Get GPU device */
    err = clGetDeviceIDs(platform, CL_DEVICE_TYPE_GPU, 1, &device, NULL);
    if (err != CL_SUCCESS) return -1;

    /* Create context */
    context = clCreateContext(NULL, 1, &device, NULL, NULL, &err);
    if (err != CL_SUCCESS) return -1;

    /* Create command queue */
    queue = clCreateCommandQueue(context, device, 0, &err);
    if (err != CL_SUCCESS) { clReleaseContext(context); return -1; }

    /* Load kernel source */
    if (load_kernel_source() != 0) {
        clReleaseCommandQueue(queue);
        clReleaseContext(context);
        return -1;
    }

    /* Build program */
    program = clCreateProgramWithSource(context, 1, &kernel_source, NULL, &err);
    if (err != CL_SUCCESS) { clReleaseCommandQueue(queue); clReleaseContext(context); return -1; }

    err = clBuildProgram(program, 1, &device, "-cl-std=CL1.2", NULL, NULL);
    if (err != CL_SUCCESS) {
        size_t log_size;
        clGetProgramBuildInfo(program, device, CL_PROGRAM_BUILD_LOG, 0, NULL, &log_size);
        char *log = (char *)malloc(log_size);
        if (log) {
            clGetProgramBuildInfo(program, device, CL_PROGRAM_BUILD_LOG, log_size, log, NULL);
            fprintf(stderr, "OpenCL build log:\n%s\n", log);
            free(log);
        }
        clReleaseProgram(program);
        clReleaseCommandQueue(queue);
        clReleaseContext(context);
        return -1;
    }

    /* Create kernel */
    kernel = clCreateKernel(program, "yespower_kernel", &err);
    if (err != CL_SUCCESS) {
        clReleaseProgram(program);
        clReleaseCommandQueue(queue);
        clReleaseContext(context);
        return -1;
    }

    /* Determine max batch size from device memory */
    cl_ulong global_mem;
    clGetDeviceInfo(device, CL_DEVICE_GLOBAL_MEM_SIZE, sizeof(global_mem), &global_mem, NULL);
    size_t usable = (size_t)(global_mem * 8 / 10);
    gpu_max_batch = (int)(usable / PER_THREAD);
    if (gpu_max_batch < 1) gpu_max_batch = 1;
    if (gpu_max_batch > 2048) gpu_max_batch = 2048;

    /* Allocate device buffers */
    size_t scratch_sz = (size_t)gpu_max_batch * PER_THREAD;
    d_scratch = clCreateBuffer(context, CL_MEM_READ_WRITE, scratch_sz, NULL, &err);
    if (err != CL_SUCCESS) goto fail;
    d_headers = clCreateBuffer(context, CL_MEM_READ_ONLY, (size_t)gpu_max_batch * 80, NULL, &err);
    if (err != CL_SUCCESS) goto fail;
    d_outputs = clCreateBuffer(context, CL_MEM_WRITE_ONLY, (size_t)gpu_max_batch * 32, NULL, &err);
    if (err != CL_SUCCESS) goto fail;

    initialized = 1;
    return 1; /* one device */

fail:
    if (d_scratch) clReleaseMemObject(d_scratch);
    if (d_headers) clReleaseMemObject(d_headers);
    if (d_outputs) clReleaseMemObject(d_outputs);
    clReleaseKernel(kernel);
    clReleaseProgram(program);
    clReleaseCommandQueue(queue);
    clReleaseContext(context);
    return -1;
}

int gpu_device_info(int device_id, char *name, size_t name_size,
                    size_t *global_mem) {
    (void)device_id; /* only one device supported */
    cl_int err;
    err = clGetDeviceInfo(device, CL_DEVICE_NAME, name_size - 1, name, NULL);
    if (err != CL_SUCCESS) return -1;
    name[name_size - 1] = '\0';
    if (global_mem) {
        cl_ulong mem;
        clGetDeviceInfo(device, CL_DEVICE_GLOBAL_MEM_SIZE, sizeof(mem), &mem, NULL);
        *global_mem = (size_t)mem;
    }
    return 0;
}

int gpu_hash(const uint8_t *headers, uint8_t *outputs, int count,
             const uint8_t *pers, int perslen) {
    if (!initialized) return -1;
    if (count > gpu_max_batch) count = gpu_max_batch;
    cl_int err;

    /* Write input data */
    err = clEnqueueWriteBuffer(queue, d_headers, CL_TRUE, 0,
                               (size_t)count * 80, headers, 0, NULL, NULL);
    if (err != CL_SUCCESS) return -1;

    /* Handle personalization string */
    if (perslen + 1 > pers_cap) {
        if (d_pers) clReleaseMemObject(d_pers);
        pers_cap = perslen + 1;
        d_pers = clCreateBuffer(context, CL_MEM_READ_ONLY, pers_cap, NULL, &err);
        if (err != CL_SUCCESS) return -1;
    }
    err = clEnqueueWriteBuffer(queue, d_pers, CL_TRUE, 0, perslen, pers, 0, NULL, NULL);
    if (err != CL_SUCCESS) return -1;

    /* Set kernel arguments */
    err  = clSetKernelArg(kernel, 0, sizeof(cl_mem), &d_headers);
    err |= clSetKernelArg(kernel, 1, sizeof(cl_mem), &d_outputs);
    err |= clSetKernelArg(kernel, 2, sizeof(int),    &count);
    err |= clSetKernelArg(kernel, 3, sizeof(cl_mem), &d_pers);
    err |= clSetKernelArg(kernel, 4, sizeof(int),    &perslen);
    err |= clSetKernelArg(kernel, 5, sizeof(cl_mem), &d_scratch);
    if (err != CL_SUCCESS) return -1;

    /* Launch kernel */
    size_t global_work = (size_t)count;
    size_t local_work  = 256;
    err = clEnqueueNDRangeKernel(queue, kernel, 1, NULL,
                                 &global_work, &local_work, 0, NULL, NULL);
    if (err != CL_SUCCESS) return -1;

    /* Read results */
    err = clEnqueueReadBuffer(queue, d_outputs, CL_TRUE, 0,
                              (size_t)count * 32, outputs, 0, NULL, NULL);
    if (err != CL_SUCCESS) return -1;

    return 0;
}

void gpu_close(void) {
    if (d_scratch) clReleaseMemObject(d_scratch);
    if (d_headers) clReleaseMemObject(d_headers);
    if (d_outputs) clReleaseMemObject(d_outputs);
    if (d_pers)    clReleaseMemObject(d_pers);
    if (kernel)    clReleaseKernel(kernel);
    if (program)   clReleaseProgram(program);
    if (queue)     clReleaseCommandQueue(queue);
    if (context)   clReleaseContext(context);
    d_scratch = NULL; d_headers = NULL; d_outputs = NULL; d_pers = NULL;
    kernel = NULL; program = NULL; queue = NULL; context = NULL;
    initialized = 0; gpu_max_batch = 0; pers_cap = 0;
}
