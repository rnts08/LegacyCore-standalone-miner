#ifndef GPU_BRIDGE_H
#define GPU_BRIDGE_H

#include <stdint.h>
#include <stddef.h>

/* Initialize GPU device.  Returns number of devices found, or -1 on error. */
int gpu_init(void);

/* Get device info (name, memory).  Returns 0 on success. */
int gpu_device_info(int device_id, char *name, size_t name_size,
                    size_t *global_mem);

/* Launch kernels for a batch of nonces.
 *
 * headers     — [count][80] serialized block headers (each with a unique nonce)
 * outputs     — [count][32] output hashes (written by GPU)
 * count       — number of hashes in this batch
 * pers        — personalization string (e.g. "LegacyCoinPoW")
 * perslen     — length of pers
 *
 * Returns 0 on success, -1 on error.
 */
int gpu_hash(const uint8_t *headers, uint8_t *outputs, int count,
             const uint8_t *pers, int perslen);

/* Release GPU resources. */
void gpu_close(void);

/* Maximum batch size (limited by GPU memory).  Set by gpu_init(). */
extern int gpu_max_batch;

#endif /* GPU_BRIDGE_H */
