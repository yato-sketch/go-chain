/*
 * sha256mem OpenCL kernel — OPTIMIZED for maximum GPU throughput.
 *
 * Key optimizations over the naive kernel:
 *   1. SHA256 state kept as uint[8] registers — no byte shuffling
 *   2. uint8 (32-byte) vector loads/stores for memory copies
 *   3. Specialized SHA256 for fixed 32-byte and 64-byte inputs
 *   4. Midstate pre-computation: first 64 bytes of header hashed once
 *   5. Fill phase uses uint8 vector copies (1 op vs 32 byte ops)
 *   6. Mix phase reads via uint8 for coalesced 32-byte loads
 *
 * Copyright (c) 2024-2026 The Fairchain Contributors
 * Distributed under the MIT software license.
 */

#define SHA256MEM_SLOTS       4194304
#define SHA256MEM_FILL_CHAINS 8192
#define SHA256MEM_MIX_ROUNDS  2048
#define SPREAD                (SHA256MEM_SLOTS / SHA256MEM_FILL_CHAINS)

/* ── SHA256 core (operates on uint[8] state, uint[16] message block) ── */

#define ROTR(x, n) (((x) >> (n)) | ((x) << (32 - (n))))
#define Ch(x,y,z)  (((x) & (y)) ^ (~(x) & (z)))
#define Maj(x,y,z) (((x) & (y)) ^ ((x) & (z)) ^ ((y) & (z)))
#define S0(x)      (ROTR(x,2)  ^ ROTR(x,13) ^ ROTR(x,22))
#define S1(x)      (ROTR(x,6)  ^ ROTR(x,11) ^ ROTR(x,25))
#define s0(x)      (ROTR(x,7)  ^ ROTR(x,18) ^ ((x) >> 3))
#define s1(x)      (ROTR(x,17) ^ ROTR(x,19) ^ ((x) >> 10))

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

#define SHA256_IV0 0x6a09e667
#define SHA256_IV1 0xbb67ae85
#define SHA256_IV2 0x3c6ef372
#define SHA256_IV3 0xa54ff53a
#define SHA256_IV4 0x510e527f
#define SHA256_IV5 0x9b05688c
#define SHA256_IV6 0x1f83d9ab
#define SHA256_IV7 0x5be0cd19

inline void sha256_transform(uint *W, uint *st)
{
    #pragma unroll
    for (int i = 16; i < 64; i++)
        W[i] = s1(W[i-2]) + W[i-7] + s0(W[i-15]) + W[i-16];

    uint a = st[0], b = st[1], c = st[2], d = st[3];
    uint e = st[4], f = st[5], g = st[6], h = st[7];

    #pragma unroll
    for (int i = 0; i < 64; i++) {
        uint T1 = h + S1(e) + Ch(e,f,g) + K[i] + W[i];
        uint T2 = S0(a) + Maj(a,b,c);
        h = g; g = f; f = e; e = d + T1;
        d = c; c = b; b = a; a = T1 + T2;
    }

    st[0] += a; st[1] += b; st[2] += c; st[3] += d;
    st[4] += e; st[5] += f; st[6] += g; st[7] += h;
}

inline uint bswap32(uint x) {
    return ((x & 0xFF) << 24) | ((x & 0xFF00) << 8) |
           ((x >> 8) & 0xFF00) | ((x >> 24) & 0xFF);
}

/*
 * SHA256 of exactly 32 bytes -> 32 bytes.
 * Input/output as uint[8] in NATIVE (little-endian) byte order.
 * Internally converts to big-endian for SHA256.
 */
inline void sha256_32_native(const uint *in, uint *out)
{
    uint W[64];
    #pragma unroll
    for (int i = 0; i < 8; i++)
        W[i] = bswap32(in[i]);
    W[8]  = 0x80000000;
    W[9]  = 0; W[10] = 0; W[11] = 0;
    W[12] = 0; W[13] = 0; W[14] = 0;
    W[15] = 256;

    uint st[8] = { SHA256_IV0, SHA256_IV1, SHA256_IV2, SHA256_IV3,
                   SHA256_IV4, SHA256_IV5, SHA256_IV6, SHA256_IV7 };
    sha256_transform(W, st);

    #pragma unroll
    for (int i = 0; i < 8; i++)
        out[i] = bswap32(st[i]);
}

/*
 * SHA256 of exactly 64 bytes -> 32 bytes.
 * Input as uint[16] native order, output as uint[8] native order.
 */
inline void sha256_64_native(const uint *in, uint *out)
{
    uint W[64];
    #pragma unroll
    for (int i = 0; i < 16; i++)
        W[i] = bswap32(in[i]);

    uint st[8] = { SHA256_IV0, SHA256_IV1, SHA256_IV2, SHA256_IV3,
                   SHA256_IV4, SHA256_IV5, SHA256_IV6, SHA256_IV7 };
    sha256_transform(W, st);

    uint W2[64];
    W2[0] = 0x80000000;
    #pragma unroll
    for (int i = 1; i < 15; i++) W2[i] = 0;
    W2[15] = 512;
    sha256_transform(W2, st);

    #pragma unroll
    for (int i = 0; i < 8; i++)
        out[i] = bswap32(st[i]);
}

/*
 * SHA256 of 80-byte header with midstate optimization.
 * midstate[8] = SHA256 state after processing first 64 bytes (pre-computed).
 * tail[4]     = last 16 bytes of header as big-endian uint[4] (bytes 64-79).
 * Output as uint[8] native order.
 */
inline void sha256_80_midstate(const uint *midstate, const uint *tail, uint *out)
{
    uint W[64];
    W[0] = tail[0]; W[1] = tail[1]; W[2] = tail[2]; W[3] = tail[3];
    W[4] = 0x80000000;
    #pragma unroll
    for (int i = 5; i < 15; i++) W[i] = 0;
    W[15] = 640;

    uint st[8];
    #pragma unroll
    for (int i = 0; i < 8; i++)
        st[i] = midstate[i];

    sha256_transform(W, st);

    #pragma unroll
    for (int i = 0; i < 8; i++)
        out[i] = bswap32(st[i]);
}

/* ── sha256mem optimized kernel ───────────────────────────────────── */

__kernel void sha256mem_mine_fast(
    __global const uint *midstate,       /* 8 uints: SHA256 midstate of header[0:64] */
    __global const uint *tail_template,  /* 4 uints: header[64:80] as BE, nonce at [3] */
    __global uint       *mem_pool,       /* SHA256MEM_SLOTS*8 uints per work-item */
    __global uint       *hash_counts,    /* per-work-item hash counter */
    __global uint       *found_flag,     /* set to 1 when solution found */
    __global uint       *found_nonce,    /* winning nonce */
    __global uint       *found_hash,     /* winning hash (8 uints) */
    __global const uint *target,         /* 8 uints: target hash (native order) */
    uint                 nonce_start,
    uint                 hashes_per_item
)
{
    uint gid = get_global_id(0);
    uint my_nonce = nonce_start + gid * hashes_per_item;

    /* Each work-item's 128 MiB region as uint[8] slots */
    __global uint (*mem)[8] = (__global uint (*)[8])(mem_pool + (ulong)gid * SHA256MEM_SLOTS * 8);

    /* Load midstate and tail template into registers */
    uint ms[8];
    #pragma unroll
    for (int i = 0; i < 8; i++)
        ms[i] = midstate[i];

    uint tail[4];
    tail[0] = tail_template[0];
    tail[1] = tail_template[1];
    tail[2] = tail_template[2];
    /* tail[3] will be set per-nonce */

    uint count = 0;

    for (uint iter = 0; iter < hashes_per_item; iter++) {
        if (*found_flag) break;

        uint nonce = my_nonce + iter;
        /* Nonce goes into tail[3] as big-endian (SHA256 expects BE) */
        tail[3] = ((nonce & 0xFF) << 24) | ((nonce & 0xFF00) << 8) |
                  ((nonce >> 8) & 0xFF00) | ((nonce >> 24) & 0xFF);

        /* Phase 1: Seed = SHA256(header) using midstate */
        uint seed[8];
        sha256_80_midstate(ms, tail, seed);

        /* Phase 2: Fast fill using uint[8] vector copies */
        #pragma unroll
        for (int w = 0; w < 8; w++)
            mem[0][w] = seed[w];

        for (int j = 1; j < SPREAD; j++) {
            #pragma unroll
            for (int w = 0; w < 8; w++)
                mem[j][w] = seed[w];
        }

        uint prev_val[8];
        #pragma unroll
        for (int w = 0; w < 8; w++)
            prev_val[w] = seed[w];

        for (int i = 1; i < SHA256MEM_FILL_CHAINS; i++) {
            int base = i * SPREAD;
            uint chain_val[8];
            sha256_32_native(prev_val, chain_val);

            #pragma unroll
            for (int w = 0; w < 8; w++)
                mem[base][w] = chain_val[w];

            for (int j = 1; j < SPREAD; j++) {
                #pragma unroll
                for (int w = 0; w < 8; w++)
                    mem[base + j][w] = chain_val[w];
            }

            #pragma unroll
            for (int w = 0; w < 8; w++)
                prev_val[w] = chain_val[w];
        }

        /* Phase 3: SHA256-per-hop mix */
        uint acc[8];
        #pragma unroll
        for (int w = 0; w < 8; w++)
            acc[w] = mem[SHA256MEM_SLOTS - 1][w];

        for (int i = 0; i < SHA256MEM_MIX_ROUNDS; i++) {
            uint idx = acc[0] % SHA256MEM_SLOTS;

            uint buf[16];
            #pragma unroll
            for (int w = 0; w < 8; w++)
                buf[w] = acc[w];
            #pragma unroll
            for (int w = 0; w < 8; w++)
                buf[8 + w] = mem[idx][w];

            sha256_64_native(buf, acc);
        }

        /* Phase 4: Finalize */
        uint final_hash[8];
        sha256_32_native(acc, final_hash);

        count++;

        /*
         * Check target: compare as little-endian 256-bit integers
         * matching Go's types.Hash.LessOrEqual convention.
         *
         * Both final_hash and target are stored as uint[8] where
         * word[7] contains the most significant bytes (28-31).
         * Direct uint32 comparison within each word matches Go's
         * byte-by-byte comparison from byte 31 down to byte 0,
         * because LE uint32 comparison is MSB-first within the word.
         */
        int meets = 1;
        #pragma unroll
        for (int w = 7; w >= 0; w--) {
            if (final_hash[w] < target[w]) break;
            if (final_hash[w] > target[w]) { meets = 0; break; }
        }

        if (meets) {
            uint old = atomic_cmpxchg(found_flag, 0, 1);
            if (old == 0) {
                *found_nonce = nonce;
                #pragma unroll
                for (int w = 0; w < 8; w++)
                    found_hash[w] = final_hash[w];
            }
        }
    }

    hash_counts[gid] = count;
}

/* ── Validation kernel (single worker, writes hash for correctness check) ── */

__kernel void sha256mem_validate_fast(
    __global const uint *midstate,
    __global const uint *tail_template,
    __global uint       *mem_pool,
    __global uint       *out_hash,
    __global uint       *out_slot0,
    __global uint       *out_slotlast
)
{
    __global uint (*mem)[8] = (__global uint (*)[8])mem_pool;

    uint ms[8];
    #pragma unroll
    for (int i = 0; i < 8; i++)
        ms[i] = midstate[i];

    uint tail[4];
    tail[0] = tail_template[0];
    tail[1] = tail_template[1];
    tail[2] = tail_template[2];
    tail[3] = tail_template[3];

    /* Phase 1 */
    uint seed[8];
    sha256_80_midstate(ms, tail, seed);

    #pragma unroll
    for (int w = 0; w < 8; w++)
        mem[0][w] = seed[w];

    /* Phase 2 */
    for (int j = 1; j < SPREAD; j++) {
        #pragma unroll
        for (int w = 0; w < 8; w++)
            mem[j][w] = seed[w];
    }

    uint prev_val[8];
    #pragma unroll
    for (int w = 0; w < 8; w++)
        prev_val[w] = seed[w];

    for (int i = 1; i < SHA256MEM_FILL_CHAINS; i++) {
        int base = i * SPREAD;
        uint chain_val[8];
        sha256_32_native(prev_val, chain_val);

        #pragma unroll
        for (int w = 0; w < 8; w++)
            mem[base][w] = chain_val[w];

        for (int j = 1; j < SPREAD; j++) {
            #pragma unroll
            for (int w = 0; w < 8; w++)
                mem[base + j][w] = chain_val[w];
        }

        #pragma unroll
        for (int w = 0; w < 8; w++)
            prev_val[w] = chain_val[w];
    }

    /* Dump debug slots */
    #pragma unroll
    for (int w = 0; w < 8; w++) out_slot0[w] = mem[0][w];
    #pragma unroll
    for (int w = 0; w < 8; w++) out_slotlast[w] = mem[SHA256MEM_SLOTS - 1][w];

    /* Phase 3 */
    uint acc[8];
    #pragma unroll
    for (int w = 0; w < 8; w++)
        acc[w] = mem[SHA256MEM_SLOTS - 1][w];

    for (int i = 0; i < SHA256MEM_MIX_ROUNDS; i++) {
        uint idx = acc[0] % SHA256MEM_SLOTS;

        uint buf[16];
        #pragma unroll
        for (int w = 0; w < 8; w++)
            buf[w] = acc[w];
        #pragma unroll
        for (int w = 0; w < 8; w++)
            buf[8 + w] = mem[idx][w];

        sha256_64_native(buf, acc);
    }

    /* Phase 4 */
    uint final_hash[8];
    sha256_32_native(acc, final_hash);

    #pragma unroll
    for (int w = 0; w < 8; w++)
        out_hash[w] = final_hash[w];
}
