/*
 * sha256mem OpenCL kernel — maximally optimized for NVIDIA GPUs.
 *
 * Each work-item independently computes sha256mem hashes over a
 * contiguous nonce range. The 4 MiB buffer lives in global (VRAM)
 * memory per work-item.
 *
 * Copyright (c) 2024-2026 The Fairchain Contributors
 * Distributed under the MIT software license.
 */

#define SHA256MEM_SLOTS       4194304
#define SHA256MEM_FILL_CHAINS 8192
#define SHA256MEM_MIX_ROUNDS  2048

/* ── SHA256 implementation (fully unrolled for GPU) ─────────────── */

#define ROTR(x, n) (((x) >> (n)) | ((x) << (32 - (n))))
#define SHR(x, n)  ((x) >> (n))
#define Ch(x,y,z)  (((x) & (y)) ^ (~(x) & (z)))
#define Maj(x,y,z) (((x) & (y)) ^ ((x) & (z)) ^ ((y) & (z)))
#define Sigma0(x)   (ROTR(x,2)  ^ ROTR(x,13) ^ ROTR(x,22))
#define Sigma1(x)   (ROTR(x,6)  ^ ROTR(x,11) ^ ROTR(x,25))
#define sigma0(x)   (ROTR(x,7)  ^ ROTR(x,18) ^ SHR(x,3))
#define sigma1(x)   (ROTR(x,17) ^ ROTR(x,19) ^ SHR(x,10))

__constant uint K[64] = {
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5,
    0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
    0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc,
    0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
    0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
    0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3,
    0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5,
    0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
    0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2
};

inline void sha256_transform(const uint *block, uint *state)
{
    uint W[64];
    #pragma unroll
    for (int i = 0; i < 16; i++)
        W[i] = block[i];
    #pragma unroll
    for (int i = 16; i < 64; i++)
        W[i] = sigma1(W[i-2]) + W[i-7] + sigma0(W[i-15]) + W[i-16];

    uint a = state[0], b = state[1], c = state[2], d = state[3];
    uint e = state[4], f = state[5], g = state[6], h = state[7];

    #pragma unroll
    for (int i = 0; i < 64; i++) {
        uint T1 = h + Sigma1(e) + Ch(e,f,g) + K[i] + W[i];
        uint T2 = Sigma0(a) + Maj(a,b,c);
        h = g; g = f; f = e; e = d + T1;
        d = c; c = b; b = a; a = T1 + T2;
    }

    state[0] += a; state[1] += b; state[2] += c; state[3] += d;
    state[4] += e; state[5] += f; state[6] += g; state[7] += h;
}

/* SHA256 of exactly 32 bytes (one padded block = 64 bytes) */
inline void sha256_32(const uchar *in, uchar *out)
{
    uint block[16];
    #pragma unroll
    for (int i = 0; i < 8; i++)
        block[i] = ((uint)in[i*4] << 24) | ((uint)in[i*4+1] << 16) |
                   ((uint)in[i*4+2] << 8) | (uint)in[i*4+3];
    block[8]  = 0x80000000;
    #pragma unroll
    for (int i = 9; i < 15; i++) block[i] = 0;
    block[15] = 256;

    uint state[8] = {
        0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
        0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19
    };
    sha256_transform(block, state);

    #pragma unroll
    for (int i = 0; i < 8; i++) {
        out[i*4]   = (uchar)(state[i] >> 24);
        out[i*4+1] = (uchar)(state[i] >> 16);
        out[i*4+2] = (uchar)(state[i] >> 8);
        out[i*4+3] = (uchar)(state[i]);
    }
}

/* SHA256 of exactly 64 bytes (two blocks: 64 data + padding block) */
inline void sha256_64(const uchar *in, uchar *out)
{
    uint block[16];
    #pragma unroll
    for (int i = 0; i < 16; i++)
        block[i] = ((uint)in[i*4] << 24) | ((uint)in[i*4+1] << 16) |
                   ((uint)in[i*4+2] << 8) | (uint)in[i*4+3];

    uint state[8] = {
        0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
        0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19
    };
    sha256_transform(block, state);

    uint block2[16];
    block2[0] = 0x80000000;
    #pragma unroll
    for (int i = 1; i < 15; i++) block2[i] = 0;
    block2[15] = 512;
    sha256_transform(block2, state);

    #pragma unroll
    for (int i = 0; i < 8; i++) {
        out[i*4]   = (uchar)(state[i] >> 24);
        out[i*4+1] = (uchar)(state[i] >> 16);
        out[i*4+2] = (uchar)(state[i] >> 8);
        out[i*4+3] = (uchar)(state[i]);
    }
}

/* SHA256 of exactly 80 bytes (block header size) */
inline void sha256_80(const uchar *in, uchar *out)
{
    uint block[16];
    #pragma unroll
    for (int i = 0; i < 16; i++)
        block[i] = ((uint)in[i*4] << 24) | ((uint)in[i*4+1] << 16) |
                   ((uint)in[i*4+2] << 8) | (uint)in[i*4+3];

    uint state[8] = {
        0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
        0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19
    };
    sha256_transform(block, state);

    uint block2[16];
    #pragma unroll
    for (int i = 0; i < 4; i++)
        block2[i] = ((uint)in[64+i*4] << 24) | ((uint)in[64+i*4+1] << 16) |
                    ((uint)in[64+i*4+2] << 8) | (uint)in[64+i*4+3];
    block2[4] = 0x80000000;
    #pragma unroll
    for (int i = 5; i < 15; i++) block2[i] = 0;
    block2[15] = 640;
    sha256_transform(block2, state);

    #pragma unroll
    for (int i = 0; i < 8; i++) {
        out[i*4]   = (uchar)(state[i] >> 24);
        out[i*4+1] = (uchar)(state[i] >> 16);
        out[i*4+2] = (uchar)(state[i] >> 8);
        out[i*4+3] = (uchar)(state[i]);
    }
}

/* ── sha256mem kernel ───────────────────────────────────────────── */

__kernel void sha256mem_mine(
    __global const uchar *header,       /* 80-byte block header template */
    __global uchar       *mem_pool,     /* SHA256MEM_SLOTS*32 bytes per work-item */
    __global uint        *hash_counts,  /* per-work-item hash counter */
    __global uint        *found_flag,   /* set to 1 when solution found */
    __global uint        *found_nonce,  /* winning nonce */
    __global uchar       *found_hash,   /* winning hash (32 bytes) */
    uint                  nonce_start,  /* starting nonce for this batch */
    uint                  hashes_per_item /* how many nonces each item tries */
)
{
    uint gid = get_global_id(0);
    uint my_nonce = nonce_start + gid * hashes_per_item;

    /* Each work-item gets its own 4 MiB region in the memory pool */
    __global uchar (*mem)[32] = (__global uchar (*)[32])(mem_pool + (ulong)gid * SHA256MEM_SLOTS * 32);

    uchar hdr[80];
    for (int i = 0; i < 80; i++)
        hdr[i] = header[i];

    uint count = 0;

    for (uint iter = 0; iter < hashes_per_item; iter++) {
        if (*found_flag) break;

        uint nonce = my_nonce + iter;
        /* Write nonce into header bytes 76-79 (little-endian) */
        hdr[76] = (uchar)(nonce);
        hdr[77] = (uchar)(nonce >> 8);
        hdr[78] = (uchar)(nonce >> 16);
        hdr[79] = (uchar)(nonce >> 24);

        /* Phase 1: Seed */
        uchar slot[32];
        sha256_80(hdr, slot);
        for (int b = 0; b < 32; b++) mem[0][b] = slot[b];

        /* Phase 2: Fast fill — chain FILL_CHAINS SHA256s, copy to spread */
        for (int b = 0; b < 32; b++) mem[0][b] = slot[b];
        int spread = SHA256MEM_SLOTS / SHA256MEM_FILL_CHAINS;
        for (int j = 1; j < spread; j++)
            for (int b = 0; b < 32; b++) mem[j][b] = mem[0][b];

        for (int i = 1; i < SHA256MEM_FILL_CHAINS; i++) {
            int base = i * spread;
            int prev = (i - 1) * spread;
            uchar prev_data[32];
            for (int b = 0; b < 32; b++) prev_data[b] = mem[prev][b];
            sha256_32(prev_data, slot);
            for (int b = 0; b < 32; b++) mem[base][b] = slot[b];
            for (int j = 1; j < spread; j++)
                for (int b = 0; b < 32; b++) mem[base + j][b] = mem[base][b];
        }

        /* Phase 3: SHA256-per-hop mix — read, hash, derive next address */
        uchar acc[32];
        for (int b = 0; b < 32; b++) acc[b] = mem[SHA256MEM_SLOTS-1][b];

        for (int i = 0; i < SHA256MEM_MIX_ROUNDS; i++) {
            uint idx = ((uint)acc[0]) | ((uint)acc[1] << 8) |
                       ((uint)acc[2] << 16) | ((uint)acc[3] << 24);
            idx %= SHA256MEM_SLOTS;

            uchar buf[64];
            for (int b = 0; b < 32; b++) buf[b] = acc[b];
            for (int b = 0; b < 32; b++) buf[32+b] = mem[idx][b];
            sha256_64(buf, acc);
        }

        /* Phase 4: Finalize */
        uchar final_hash[32];
        sha256_32(acc, final_hash);

        count++;

        /* Check if hash meets target (simplified: check leading zero bytes) */
        /* For benchmarking we just count — no target check needed */
    }

    hash_counts[gid] = count;
}
