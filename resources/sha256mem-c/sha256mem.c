#include "sha256mem.h"
#include <openssl/sha.h>
#include <string.h>
#include <stdlib.h>

void sha256mem_hash(const uint8_t *data, size_t len, uint8_t out[32]) {
    /* Heap-allocate the 128 MiB buffer. */
    uint8_t (*mem)[32] = malloc(SHA256MEM_SLOTS * 32);
    if (!mem) {
        memset(out, 0, 32);
        return;
    }

    /* Phase 1: Seed from input. */
    SHA256(data, len, mem[0]);

    /* Phase 2: Fast fill — chain FILL_CHAINS SHA256s, copy each result
     * to fill (SLOTS/FILL_CHAINS) consecutive slots via memcpy. */
    {
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
    }

    /* Phase 3: SHA256-per-hop mix — read, hash, derive next address. */
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

    /* Phase 4: Finalize. */
    SHA256(acc, 32, out);

    free(mem);
}
