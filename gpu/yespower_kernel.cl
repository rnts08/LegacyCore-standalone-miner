/*
 * yespower 1.0 OpenCL kernel for LegacyCoin.
 *
 * Each work-item processes one nonce.  Scratch (~8 MB) lives in global
 * memory.  Designed for correctness first.
 */

/* -------------------------------------------------------------------- */
/*  Constants                                                            */
/* -------------------------------------------------------------------- */
#define YESPOWER_1_0     10
#define N                2048
#define R                  32
#define PWXsimple           2
#define PWXgather           4
#define Swidth_1_0         11
#define PWXbytes   (PWXgather * PWXsimple * 8)  /* 64 */
#define Sbytes     (3 * ((1 << Swidth_1_0) * PWXsimple * 8))  /* 98304 */
#define S_mask     (((1 << Swidth_1_0) - 1) * PWXsimple * 8)  /* 32752 */
#define S_mask2    ((ulong)S_mask << 32 | (ulong)S_mask)

#define B_SIZE     (128 * R)          /* 4096 */
#define V_SIZE     (B_SIZE * N)       /* 8388608 */
#define XY_SIZE    (B_SIZE + 64)      /* 4160 */
#define PER_THREAD (B_SIZE + V_SIZE + XY_SIZE + Sbytes)  /* ~8.5 MB */

/* -------------------------------------------------------------------- */
/*  Little-endian helpers                                               */
/* -------------------------------------------------------------------- */
inline uint le32dec(const uchar *p) {
    return (uint)p[0] | ((uint)p[1] << 8) | ((uint)p[2] << 16) | ((uint)p[3] << 24);
}
inline void le32enc(uchar *p, uint v) {
    p[0] = v; p[1] = v >> 8; p[2] = v >> 16; p[3] = v >> 24;
}
inline void le64enc(uchar *p, ulong v) {
    le32enc(p, (uint)v); le32enc(p + 4, (uint)(v >> 32));
}

/* -------------------------------------------------------------------- */
/*  SHA256 core                                                         */
/* -------------------------------------------------------------------- */
inline uint rotl32(uint x, uint n) { return (x << n) | (x >> (32 - n)); }
inline uint rotr32(uint x, uint n) { return (x >> n) | (x << (32 - n)); }
inline uint Ch(uint x, uint y, uint z)  { return (x & (y ^ z)) ^ z; }
inline uint Maj(uint x, uint y, uint z) { return (x & y) | (z & (x | y)); }
inline uint SIG0(uint x) { return rotr32(x, 2) ^ rotr32(x,13) ^ rotr32(x,22); }
inline uint SIG1(uint x) { return rotr32(x, 6) ^ rotr32(x,11) ^ rotr32(x,25); }
inline uint sig0(uint x) { return rotr32(x, 7) ^ rotr32(x,18) ^ (x >> 3); }
inline uint sig1(uint x) { return rotr32(x,17) ^ rotr32(x,19) ^ (x >>10); }

__constant uint K256[64] = {
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

void sha256_transform(__private uint state[8], __private const uchar block[64]) {
    uint W[64], S[8];
    for (int i = 0; i < 16; i++)
        W[i] = le32dec(block + i * 4);
    for (int i = 16; i < 64; i++)
        W[i] = sig1(W[i-2]) + W[i-7] + sig0(W[i-15]) + W[i-16];

    for (int i = 0; i < 8; i++) S[i] = state[i];

    for (int i = 0; i < 64; i++) {
        uint t1 = S[7] + SIG1(S[4]) + Ch(S[4], S[5], S[6]) + K256[i] + W[i];
        uint t2 = SIG0(S[0]) + Maj(S[0], S[1], S[2]);
        S[7] = S[6]; S[6] = S[5]; S[5] = S[4]; S[4] = S[3] + t1;
        S[3] = S[2]; S[2] = S[1]; S[1] = S[0]; S[0] = t1 + t2;
    }

    for (int i = 0; i < 8; i++) state[i] += S[i];
}

void sha256_hash(__private const uchar *in, size_t len, __private uchar out[32]) {
    uint state[8] = {
        0x6A09E667, 0xBB67AE85, 0x3C6EF372, 0xA54FF53A,
        0x510E527F, 0x9B05688C, 0x1F83D9AB, 0x5BE0CD19
    };
    uchar buf[64];
    ulong bitlen = len * 8;
    size_t off = 0;

    while (len >= 64) {
        sha256_transform(state, in + off);
        off += 64; len -= 64;
    }
    memcpy(buf, in + off, len);
    buf[len] = 0x80;
    size_t pad = (len < 56) ? (56 - len) : (64 - len + 56);
    for (size_t i = 0; i < pad; i++)
        buf[len + 1 + i] = 0;
    le64enc(buf + 56, bitlen);
    sha256_transform(state, buf);
    if (len >= 56) {
        memset(buf, 0, 56);
        le64enc(buf + 56, bitlen);
        sha256_transform(state, buf);
    }
    for (int i = 0; i < 8; i++)
        le32enc(out + i * 4, state[i]);
}

void hmac_sha256(__private const uchar *key, size_t klen,
                 __private const uchar *in, size_t inlen,
                 __private uchar out[32]) {
    uchar k[64], buf[64];
    memset(k, 0, 64);
    if (klen > 64) {
        sha256_hash(key, klen, k);
    } else {
        memcpy(k, key, klen);
    }
    uchar ihash[32];
    /* inner */
    for (int i = 0; i < 64; i++) buf[i] = k[i] ^ 0x36;
    uint state[8] = {0x6A09E667,0xBB67AE85,0x3C6EF372,0xA54FF53A,
                     0x510E527F,0x9B05688C,0x1F83D9AB,0x5BE0CD19};
    ulong bitlen = (64 + inlen) * 8;
    sha256_transform(state, buf);
    uchar tmp[128]; memcpy(tmp, buf, 64);
    size_t r = 64;
    while (inlen >= 64 - r) {
        memcpy(tmp + r, in, 64 - r);
        sha256_transform(state, tmp);
        in += 64 - r; inlen -= 64 - r;
        r = 0;
    }
    memcpy(tmp + r, in, inlen);
    r += inlen;
    tmp[r] = 0x80;
    size_t pad = (r < 56) ? (56 - r) : (64 - r + 56);
    for (size_t i = 0; i < pad; i++) tmp[r + 1 + i] = 0;
    le64enc(tmp + 56, bitlen);
    sha256_transform(state, tmp);
    if (r >= 56) {
        memset(tmp, 0, 56);
        le64enc(tmp + 56, bitlen);
        sha256_transform(state, tmp);
    }
    for (int i = 0; i < 8; i++) le32enc(ihash + i * 4, state[i]);

    /* outer */
    for (int i = 0; i < 64; i++) buf[i] = k[i] ^ 0x5c;
    bitlen = (64 + 32) * 8;
    uint ost[8] = {0x6A09E667,0xBB67AE85,0x3C6EF372,0xA54FF53A,
                   0x510E527F,0x9B05688C,0x1F83D9AB,0x5BE0CD19};
    sha256_transform(ost, buf);
    uchar obuf[96];
    memcpy(obuf, buf, 64);
    memcpy(obuf + 64, ihash, 32);
    size_t olen = 96;
    size_t len_off = 0;
    while (olen >= 64) {
        sha256_transform(ost, obuf + len_off);
        len_off += 64; olen -= 64;
    }
    r = 0;
    memcpy(tmp, obuf + len_off, olen);
    tmp[olen] = 0x80;
    pad = (olen < 56) ? (56 - olen) : (64 - olen + 56);
    for (size_t i = 0; i < pad; i++) tmp[olen + 1 + i] = 0;
    le64enc(tmp + 56, bitlen);
    sha256_transform(ost, tmp);
    if (olen >= 56) {
        memset(tmp, 0, 56);
        le64enc(tmp + 56, bitlen);
        sha256_transform(ost, tmp);
    }
    for (int i = 0; i < 8; i++) le32enc(out + i * 4, ost[i]);
}

void pbkdf2_sha256_1(__private const uchar *pw, size_t pwlen,
                     __private const uchar *salt, size_t saltlen,
                     __private uchar *out, size_t dkLen) {
    uchar buf[256], tmp[32];
    for (size_t i = 0; i * 32 < dkLen; i++) {
        size_t off = 0;
        memcpy(buf, salt, saltlen);
        off += saltlen;
        uint cnt = (uint)(i + 1);
        le32enc(buf + off, cnt); off += 4;
        hmac_sha256(pw, pwlen, buf, off, tmp);
        size_t clen = dkLen - i * 32;
        if (clen > 32) clen = 32;
        memcpy(out + i * 32, tmp, clen);
    }
}

/* -------------------------------------------------------------------- */
/*  Salsa20/8 core                                                      */
/* -------------------------------------------------------------------- */
void salsa20_8(__private uint x[16]) {
    uint z[16];
    for (int i = 0; i < 16; i++) z[i] = x[i];
    for (int r = 0; r < 4; r++) {
        x[ 4] ^= rotl32(x[ 0] + x[12],  7); x[ 8] ^= rotl32(x[ 4] + x[ 0],  9);
        x[12] ^= rotl32(x[ 8] + x[ 4], 13); x[ 0] ^= rotl32(x[12] + x[ 8], 18);
        x[ 9] ^= rotl32(x[ 5] + x[ 1],  7); x[13] ^= rotl32(x[ 9] + x[ 5],  9);
        x[ 1] ^= rotl32(x[13] + x[ 9], 13); x[ 5] ^= rotl32(x[ 1] + x[13], 18);
        x[14] ^= rotl32(x[10] + x[ 6],  7); x[ 2] ^= rotl32(x[14] + x[10],  9);
        x[ 6] ^= rotl32(x[ 2] + x[14], 13); x[10] ^= rotl32(x[ 6] + x[ 2], 18);
        x[ 3] ^= rotl32(x[15] + x[11],  7); x[ 7] ^= rotl32(x[ 3] + x[15],  9);
        x[11] ^= rotl32(x[ 7] + x[ 3], 13); x[15] ^= rotl32(x[11] + x[ 7], 18);
        x[ 1] ^= rotl32(x[ 0] + x[ 3],  7); x[ 2] ^= rotl32(x[ 1] + x[ 0],  9);
        x[ 3] ^= rotl32(x[ 2] + x[ 1], 13); x[ 0] ^= rotl32(x[ 3] + x[ 2], 18);
        x[ 6] ^= rotl32(x[ 5] + x[ 4],  7); x[ 7] ^= rotl32(x[ 6] + x[ 5],  9);
        x[ 4] ^= rotl32(x[ 7] + x[ 6], 13); x[ 5] ^= rotl32(x[ 4] + x[ 7], 18);
        x[11] ^= rotl32(x[10] + x[ 9],  7); x[ 8] ^= rotl32(x[11] + x[10],  9);
        x[ 9] ^= rotl32(x[ 8] + x[11], 13); x[10] ^= rotl32(x[ 9] + x[ 8], 18);
        x[12] ^= rotl32(x[15] + x[14],  7); x[13] ^= rotl32(x[12] + x[15],  9);
        x[14] ^= rotl32(x[13] + x[12], 13); x[15] ^= rotl32(x[14] + x[13], 18);
    }
    for (int i = 0; i < 16; i++) x[i] += z[i];
}

inline void salsa_shuffle(__private const uint w[16], __private ulong d[8]) {
    d[0] = (ulong)w[0] | ((ulong)w[5] << 32);
    d[1] = (ulong)w[10] | ((ulong)w[15] << 32);
    d[2] = (ulong)w[4] | ((ulong)w[9] << 32);
    d[3] = (ulong)w[14] | ((ulong)w[3] << 32);
    d[4] = (ulong)w[8] | ((ulong)w[13] << 32);
    d[5] = (ulong)w[2] | ((ulong)w[7] << 32);
    d[6] = (ulong)w[12] | ((ulong)w[1] << 32);
    d[7] = (ulong)w[6] | ((ulong)w[11] << 32);
}

inline void salsa_unshuffle(__private const ulong d[8], __private uint w[16]) {
    w[0]  = (uint)d[0]; w[1]  = (uint)(d[6] >> 32);
    w[2]  = (uint)d[5]; w[3]  = (uint)(d[3] >> 32);
    w[4]  = (uint)d[2]; w[5]  = (uint)(d[0] >> 32);
    w[6]  = (uint)d[7]; w[7]  = (uint)(d[5] >> 32);
    w[8]  = (uint)d[4]; w[9]  = (uint)(d[2] >> 32);
    w[10] = (uint)d[1]; w[11] = (uint)(d[7] >> 32);
    w[12] = (uint)d[6]; w[13] = (uint)(d[4] >> 32);
    w[14] = (uint)d[3]; w[15] = (uint)(d[1] >> 32);
}

/* -------------------------------------------------------------------- */
/*  pwxform                                                             */
/* -------------------------------------------------------------------- */
ulong pwxform_lookup(ulong x0, ulong x1,
                     __global const uchar *S0, __global const uchar *S1) {
    ulong x = x0 & S_mask2;
    uint lo = (uint)x;
    uint hi = (uint)(x >> 32);
    ulong p00 = *(__global const ulong *)(S0 + lo);
    ulong p01 = *(__global const ulong *)(S0 + lo + 8);
    ulong p10 = *(__global const ulong *)(S1 + hi);
    ulong p11 = *(__global const ulong *)(S1 + hi + 8);
    ulong r0 = ((x0 >> 32) * (uint)x0 + p00) ^ p10;
    ulong r1 = ((x1 >> 32) * (uint)x1 + p01) ^ p11;
    return (r1 << 32) | (uint)r0;
}

void pwxform_round(__private ulong d[8],
                   __global uchar *S0, __global uchar *S1) {
    for (int i = 0; i < 8; i += 2) {
        ulong v = pwxform_lookup(d[i], d[i+1], S0, S1);
        d[i] = v;
        d[i+1] = v >> 32;
    }
}

void pwxform_round_write4(__private ulong d[8],
                          __global uchar *S0, __global uchar *S1,
                          __private size_t *w) {
    for (int i = 0; i < 4; i += 2) {
        ulong v = pwxform_lookup(d[i], d[i+1], S0, S1);
        d[i] = v; d[i+1] = v >> 32;
        *(__global ulong *)(S0 + *w) = d[i];
        *(__global ulong *)(S0 + *w + 8) = d[i+1];
        *w += 16;
    }
}

void pwxform_round_write2(__private ulong d[8],
                          __global uchar *S0, __global uchar *S1,
                          __private size_t *w) {
    for (int i = 0; i < 4; i += 2) {
        ulong v = pwxform_lookup(d[i], d[i+1], S0, S1);
        d[i] = v; d[i+1] = v >> 32;
        if (i < 2) {
            *(__global ulong *)(S0 + *w) = d[i];
            *(__global ulong *)(S0 + *w + 8) = d[i+1];
            *w += 16;
        }
    }
}

void pwxform_full(__private ulong d[8],
                  __global uchar *S0, __global uchar *S1, __global uchar *S2,
                  __private size_t *w) {
    for (int i = 0; i < 3; i++)
        pwxform_round(d, S0, S1);
    pwxform_round_write4(d, S0, S1, w);
    pwxform_round_write2(d, S0, S1, w);
    pwxform_round_write2(d, S0, S1, w);
    *w &= S_mask;
    __global uchar *tmp = S2; S2 = S1; S1 = S0; S0 = tmp; (void)S2;
}

/* -------------------------------------------------------------------- */
/*  Blockmix variants                                                   */
/* -------------------------------------------------------------------- */
inline void load_block(__private const uchar *src, __private ulong block[8]) {
    uint w[16];
    for (int i = 0; i < 16; i++)
        w[i] = le32dec(src + i * 4);
    salsa_shuffle(w, block);
}

inline void store_block(__private uchar *dst, __private const ulong block[8]) {
    uint w[16];
    salsa_unshuffle(block, w);
    for (int i = 0; i < 16; i++)
        le32enc(dst + i * 4, w[i]);
}

void blockmix_pass2(__global ulong *X, int r,
                    __global ulong *Bout, int boutIdx,
                    __global uchar *S0, __global uchar *S1, __global uchar *S2,
                    __private size_t *w) {
    int rr = r * 2 - 1;
    __private ulong x[8];
    for (int i = 0; i < 8; i++) x[i] = X[(rr)*8 + i];
    int i = 0;
    for (;;) {
        for (int k = 0; k < 8; k++) x[k] ^= X[i * 8 + k];
        pwxform_full(x, S0, S1, S2, w);
        if (i >= rr) break;
        for (int k = 0; k < 8; k++) Bout[boutIdx + i] = x[k];
        i++;
    }
    uint ws[16];
    salsa_unshuffle(x, ws);
    salsa20_8(ws);
    __private ulong bout_x[8];
    salsa_shuffle(ws, bout_x);
    for (int k = 0; k < 8; k++)
        Bout[boutIdx + i] = bout_x[k];
}

uint blockmix_xor_pass2(__global ulong *Bin1, int idx1,
                        __global const ulong *Bin2, int idx2,
                        __global ulong *Bout, int outIdx,
                        int r,
                        __global uchar *S0, __global uchar *S1, __global uchar *S2,
                        __private size_t *w) {
    int rr = r * 2 - 1;
    __private ulong x[8];
    for (int k = 0; k < 8; k++)
        x[k] = Bin1[(idx1 + rr) * 8 + k] ^ Bin2[(idx2 + rr) * 8 + k];

    int i = 0;
    rr--;
    for (;;) {
        for (int k = 0; k < 8; k++) x[k] ^= Bin1[(idx1 + i) * 8 + k];
        for (int k = 0; k < 8; k++) x[k] ^= Bin2[(idx2 + i) * 8 + k];
        pwxform_full(x, S0, S1, S2, w);
        for (int k = 0; k < 8; k++) Bout[(outIdx + i) * 8 + k] = x[k];

        for (int k = 0; k < 8; k++) x[k] ^= Bin1[(idx1 + i + 1) * 8 + k];
        for (int k = 0; k < 8; k++) x[k] ^= Bin2[(idx2 + i + 1) * 8 + k];
        pwxform_full(x, S0, S1, S2, w);
        if (i >= rr) break;
        for (int k = 0; k < 8; k++) Bout[(outIdx + i + 1) * 8 + k] = x[k];
        i += 2;
    }
    i++;
    uint ws[16];
    salsa_unshuffle(x, ws);
    salsa20_8(ws);
    __private ulong bout_x[8];
    salsa_shuffle(ws, bout_x);
    for (int k = 0; k < 8; k++)
        Bout[(outIdx + i) * 8 + k] = bout_x[k];
    return (uint)bout_x[0];
}

uint blockmix_xor_save_pass2(__global ulong *Bin1out, int idx1,
                             __global const ulong *Bin2, int idx2,
                             int r,
                             __global uchar *S0, __global uchar *S1, __global uchar *S2,
                             __private size_t *w) {
    int rr = r * 2 - 1;
    __private ulong x[8];
    for (int k = 0; k < 8; k++)
        x[k] = Bin1out[(idx1 + rr) * 8 + k] ^ Bin2[(idx2 + rr) * 8 + k];

    int i = 0;
    rr--;
    for (;;) {
        __private ulong y[8];
        for (int k = 0; k < 8; k++)
            y[k] = Bin1out[(idx1 + i) * 8 + k] ^ Bin2[(idx2 + i) * 8 + k];
        for (int k = 0; k < 8; k++)
            Bin1out[(idx1 + i) * 8 + k] = y[k];
        for (int k = 0; k < 8; k++) x[k] ^= y[k];
        pwxform_full(x, S0, S1, S2, w);
        for (int k = 0; k < 8; k++)
            Bin1out[(idx1 + i) * 8 + k] = x[k];

        for (int k = 0; k < 8; k++)
            y[k] = Bin1out[(idx1 + i + 1) * 8 + k] ^ Bin2[(idx2 + i + 1) * 8 + k];
        for (int k = 0; k < 8; k++)
            Bin1out[(idx1 + i + 1) * 8 + k] = y[k];
        for (int k = 0; k < 8; k++) x[k] ^= y[k];
        pwxform_full(x, S0, S1, S2, w);
        if (i >= rr) break;
        for (int k = 0; k < 8; k++)
            Bin1out[(idx1 + i + 1) * 8 + k] = x[k];
        i += 2;
    }
    i++;
    uint ws[16];
    salsa_unshuffle(x, ws);
    salsa20_8(ws);
    __private ulong bout_x[8];
    salsa_shuffle(ws, bout_x);
    for (int k = 0; k < 8; k++)
        Bin1out[(idx1 + i) * 8 + k] = bout_x[k];
    return (uint)bout_x[0];
}

/* -------------------------------------------------------------------- */
/*  smix1 / smix2                                                       */
/* -------------------------------------------------------------------- */
void smix1_pass2(__global uchar *B, int r, uint N_,
                 __global ulong *V, __global ulong *XY,
                 __global uchar *S0, __global uchar *S1, __global uchar *S2,
                 __private size_t *w,
                 int init_only) {
    int s = 2 * r;
    __global ulong *X = V;
    __global ulong *Y = V + s;
    __global ulong *V_j;
    uint j, n;

    int limit = init_only ? 2 : (2 * r);
    for (uint i = 0; i < (uint)limit; i++) {
        uint tmp[16];
        for (int k = 0; k < 16; k++)
            tmp[k] = le32dec(B + i * 64 + k * 4);
        __private ulong blk[8];
        salsa_shuffle(tmp, blk);
        for (int k = 0; k < 8; k++) X[i * 8 + k] = blk[k];
    }

    if (!init_only) {
        for (uint i = 1; i < (uint)r; i++)
            blockmix_pass2(X + (i-1)*2, 1, X, i*2, S0, S1, S2, w);
    }

    blockmix_pass2(X, r, Y, 0, S0, S1, S2, w);
    X = Y + s;
    blockmix_pass2(Y, r, X, 0, S0, S1, S2, w);
    j = (uint)X[0];

    for (n = 2; n < N_; n <<= 1) {
        uint m = (n < N_ / 2) ? n : (N_ - 1 - n);
        for (uint i = 1; i < m; i += 2) {
            Y = X + s;
            j &= n - 1; j += i - 1;
            V_j = V + j * s;
            j = blockmix_xor_pass2(X, 0, V_j, 0, Y, 0, r, S0, S1, S2, w);
            j &= n - 1; j += i;
            V_j = V + j * s;
            X = Y + s;
            j = blockmix_xor_pass2(Y, 0, V_j, 0, X, 0, r, S0, S1, S2, w);
        }
    }
    n >>= 1;

    j &= n - 1;
    j += N_ - 2 - n;
    V_j = V + j * s;
    Y = X + s;
    j = blockmix_xor_pass2(X, 0, V_j, 0, Y, 0, r, S0, S1, S2, w);
    j &= n - 1;
    j += N_ - 1 - n;
    V_j = V + j * s;
    blockmix_xor_pass2(Y, 0, V_j, 0, XY, 0, r, S0, S1, S2, w);

    for (uint i = 0; i < (uint)(2 * r); i++) {
        uint tmp[16];
        salsa_unshuffle(XY + i * 8, tmp);
        for (int k = 0; k < 16; k++)
            le32enc(B + i * 64 + k * 4, tmp[k]);
    }
}

void smix2_pass2(__global uchar *B, int r, uint N_, uint Nloop,
                 __global ulong *V, __global ulong *XY,
                 __global uchar *S0, __global uchar *S1, __global uchar *S2,
                 __private size_t *w) {
    int s = 2 * r;
    __global ulong *X = XY;
    uint j;

    for (uint i = 0; i < (uint)(2 * r); i++) {
        uint tmp[16];
        for (int k = 0; k < 16; k++)
            tmp[k] = le32dec(B + i * 64 + k * 4);
        __private ulong blk[8];
        salsa_shuffle(tmp, blk);
        for (int k = 0; k < 8; k++) X[i * 8 + k] = blk[k];
    }

    j = ((uint)X[0]) & (N_ - 1);
    do {
        __global ulong *V_j = V + j * s;
        j = blockmix_xor_save_pass2(X, 0, V_j, 0, r, S0, S1, S2, w) & (N_ - 1);
        V_j = V + j * s;
        j = blockmix_xor_save_pass2(X, 0, V_j, 0, r, S0, S1, S2, w) & (N_ - 1);
        Nloop -= 2;
    } while (Nloop > 0);

    for (uint i = 0; i < (uint)(2 * r); i++) {
        uint tmp[16];
        salsa_unshuffle(X + i * 8, tmp);
        for (int k = 0; k < 16; k++)
            le32enc(B + i * 64 + k * 4, tmp[k]);
    }
}

/* -------------------------------------------------------------------- */
/*  Main yespower 1.0 entry point (called by kernel)                    */
/* -------------------------------------------------------------------- */
void yespower_hash(__private const uchar *input, size_t inputlen,
                   __private const uchar *pers, size_t perslen,
                   __global uchar *scratch,
                   __private uchar output[32]) {
    __global uchar *B   = scratch;
    __global uchar *Vb  = scratch + B_SIZE;
    __global uchar *XYb = scratch + B_SIZE + V_SIZE;
    __global uchar *Sb  = scratch + B_SIZE + V_SIZE + XY_SIZE;

    __global uchar *S0 = Sb;
    __global uchar *S1 = Sb + Sbytes / 3;
    __global uchar *S2 = Sb + 2 * Sbytes / 3;
    size_t w = 0;

    uchar sha256_digest[32];
    sha256_hash(input, inputlen, sha256_digest);

    uchar B_128[128];
    pbkdf2_sha256_1(sha256_digest, 32, pers, perslen, B_128, 128);
    for (int i = 0; i < B_SIZE; i++)
        B[i] = B_128[i];

    uchar sha256_save[32];
    for (int i = 0; i < 32; i++)
        sha256_save[i] = B[i];

    ulong *V   = (__global ulong *)Vb;
    ulong *XY  = (__global ulong *)XYb;
    uint Nloop_rw = ((N + 2) / 3) & ~(uint)1;

    smix1_pass2(B, 1, Sbytes / 128, V, XY, S0, S1, S2, &w, 1);
    smix1_pass2(B, R, N, V, XY, S0, S1, S2, &w, 0);
    smix2_pass2(B, R, N, Nloop_rw, V, XY, S0, S1, S2, &w);

    hmac_sha256(B + B_SIZE - 64, 64, sha256_save, 32, output);
    (void)perslen;
}

/* -------------------------------------------------------------------- */
/*  Kernel entry point                                                  */
/* -------------------------------------------------------------------- */
__kernel void yespower_kernel(
    __global const uchar *headers,
    __global uchar *outputs,
    int count,
    __global const uchar *pers,
    int perslen,
    __global uchar *scratch_pool
) {
    int idx = get_global_id(0);
    if (idx >= count) return;

    __global uchar *my_scratch = scratch_pool + idx * PER_THREAD;
    uchar my_output[32];

    yespower_hash(headers + idx * 80, 80, pers, perslen,
                  my_scratch, my_output);

    for (int i = 0; i < 32; i++)
        outputs[idx * 32 + i] = my_output[i];
}
