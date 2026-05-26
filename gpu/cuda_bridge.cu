/*
 * CUDA yespower miner — device kernels + host bridge.
 * Compiled with: nvcc -arch=sm_61 -O3 -c cuda_bridge.cu -o cuda_bridge.o
 */

#include "bridge.h"
#include <cuda_runtime.h>
#include <stdio.h>
#include <string.h>

/* ================================================================== */
/*  Device code (kernels and helpers)                                  */
/* ================================================================== */

/* --- constants --- */
#define N          2048
#define R            32
#define Swidth_1_0   11
#define Sbytes     (3 * ((1 << Swidth_1_0) * 2 * 8))  /* 98304 */
#define S_mask     (((1 << Swidth_1_0) - 1) * 2 * 8)  /* 32752 */
#define S_mask2    (((uint64_t)S_mask << 32) | (uint64_t)S_mask)
#define B_SIZE     (128 * R)           /* 4096 */
#define V_SIZE     (B_SIZE * N)        /* 8388608 */
#define XY_SIZE    (B_SIZE + 64)       /* 4160 */
#define PER_THREAD (B_SIZE + V_SIZE + XY_SIZE + Sbytes)

/* --- helpers --- */
__device__ __forceinline__ uint32_t le32dec_d(const uint8_t *p) {
    return (uint32_t)p[0] | ((uint32_t)p[1]<<8)|((uint32_t)p[2]<<16)|((uint32_t)p[3]<<24);
}
__device__ __forceinline__ void le32enc_d(uint8_t *p, uint32_t v) {
    p[0]=v; p[1]=v>>8; p[2]=v>>16; p[3]=v>>24;
}
__device__ __forceinline__ void le64enc_d(uint8_t *p, uint64_t v) {
    le32enc_d(p,(uint32_t)v); le32enc_d(p+4,(uint32_t)(v>>32));
}
__device__ __forceinline__ uint32_t rotr32(uint32_t x, uint32_t n) { return (x>>n)|(x<<(32-n)); }
__device__ __forceinline__ uint32_t rotl32(uint32_t x, uint32_t n) { return (x<<n)|(x>>(32-n)); }

/* --- SHA256 --- */
__constant__ uint32_t K256[64] = {
    0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,
    0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,
    0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,
    0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,
    0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,
    0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,0xd192e819,0xd6990624,0xf40e3585,0x106aa070,
    0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,
    0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2
};

__device__ void sha256_transform_d(uint32_t state[8], const uint8_t block[64]) {
    uint32_t W[64], S[8];
    for (int i=0;i<16;i++) W[i]=le32dec_d(block+i*4);
    for (int i=16;i<64;i++) W[i] = (rotr32(W[i-2],17)^rotr32(W[i-2],19)^(W[i-2]>>10))
                                 + W[i-7] + (rotr32(W[i-15],7)^rotr32(W[i-15],18)^(W[i-15]>>3)) + W[i-16];
    for (int i=0;i<8;i++) S[i]=state[i];
    for (int i=0;i<64;i++) {
        uint32_t t1 = S[7] + (rotr32(S[4],6)^rotr32(S[4],11)^rotr32(S[4],25))
                     + ((S[4]&(S[5]^S[6]))^S[6]) + K256[i] + W[i];
        uint32_t t2 = (rotr32(S[0],2)^rotr32(S[0],13)^rotr32(S[0],22))
                     + ((S[0]&S[1])|(S[2]&(S[0]|S[1])));
        S[7]=S[6];S[6]=S[5];S[5]=S[4];S[4]=S[3]+t1;
        S[3]=S[2];S[2]=S[1];S[1]=S[0];S[0]=t1+t2;
    }
    for (int i=0;i<8;i++) state[i]+=S[i];
}

__device__ void sha256_hash_d(const uint8_t *in, size_t len, uint8_t out[32]) {
    uint32_t state[8]={0x6A09E667,0xBB67AE85,0x3C6EF372,0xA54FF53A,
                       0x510E527F,0x9B05688C,0x1F83D9AB,0x5BE0CD19};
    uint8_t buf[64]; uint64_t bitlen=len*8;
    while (len>=64) { sha256_transform_d(state,in); in+=64; len-=64; }
    memcpy(buf,in,len); buf[len]=0x80;
    size_t pad=(len<56)?(56-len):(64-len+56);
    memset(buf+len+1,0,pad);
    le64enc_d(buf+56,bitlen);
    sha256_transform_d(state,buf);
    if (len>=56) { memset(buf,0,56); le64enc_d(buf+56,bitlen); sha256_transform_d(state,buf); }
    for (int i=0;i<8;i++) le32enc_d(out+i*4,state[i]);
}

__device__ void hmac_sha256_d(const uint8_t *key, size_t klen,
                              const uint8_t *in, size_t inlen, uint8_t out[32]) {
    uint8_t k[64],buf[64]; memset(k,0,64);
    if (klen>64) sha256_hash_d(key,klen,k); else memcpy(k,key,klen);
    uint8_t ihash[32];
    for (int i=0;i<64;i++) buf[i]=k[i]^0x36;
    uint32_t st[8]={0x6A09E667,0xBB67AE85,0x3C6EF372,0xA54FF53A,
                   0x510E527F,0x9B05688C,0x1F83D9AB,0x5BE0CD19};
    uint64_t bl=(64+inlen)*8;
    sha256_transform_d(st,buf);
    uint8_t tmp[128]; memcpy(tmp,buf,64); size_t r=64;
    while (inlen>=64-r) { memcpy(tmp+r,in,64-r); sha256_transform_d(st,tmp); in+=64-r; inlen-=64-r; r=0; }
    memcpy(tmp+r,in,inlen); r+=inlen; tmp[r]=0x80;
    size_t pad=(r<56)?(56-r):(64-r+56); memset(tmp+r+1,0,pad); le64enc_d(tmp+56,bl);
    sha256_transform_d(st,tmp); if (r>=56) { memset(tmp,0,56); le64enc_d(tmp+56,bl); sha256_transform_d(st,tmp); }
    for (int i=0;i<8;i++) le32enc_d(ihash+i*4,st[i]);
    for (int i=0;i<64;i++) buf[i]=k[i]^0x5c;
    bl=(64+32)*8;
    uint32_t ost[8]={0x6A09E667,0xBB67AE85,0x3C6EF372,0xA54FF53A,
                    0x510E527F,0x9B05688C,0x1F83D9AB,0x5BE0CD19};
    sha256_transform_d(ost,buf);
    size_t len_off=0, olen=96; memcpy(tmp,buf,64); memcpy(tmp+64,ihash,32);
    while (olen>=64) { sha256_transform_d(ost,tmp+len_off); len_off+=64; olen-=64; }
    r=0; memcpy(buf,tmp+len_off,olen); buf[olen]=0x80;
    pad=(olen<56)?(56-olen):(64-olen+56); memset(buf+olen+1,0,pad); le64enc_d(buf+56,bl);
    sha256_transform_d(ost,buf); if (olen>=56) { memset(buf,0,56); le64enc_d(buf+56,bl); sha256_transform_d(ost,buf); }
    for (int i=0;i<8;i++) le32enc_d(out+i*4,ost[i]);
}

__device__ void pbkdf2_sha256_1_d(const uint8_t *pw,size_t pwlen,
                                  const uint8_t *salt,size_t saltlen,
                                  uint8_t *out,size_t dkLen) {
    uint8_t buf[256],tmp[32];
    for (size_t i=0;i*32<dkLen;i++) {
        size_t off=0; memcpy(buf,salt,saltlen); off+=saltlen;
        uint32_t cnt=(uint32_t)(i+1); le32enc_d(buf+off,cnt); off+=4;
        hmac_sha256_d(pw,pwlen,buf,off,tmp);
        size_t clen=dkLen-i*32; if (clen>32) clen=32;
        memcpy(out+i*32,tmp,clen);
    }
}

/* --- Salsa20/8 --- */
__device__ void salsa20_8_d(uint32_t x[16]) {
    uint32_t z[16]; memcpy(z,x,64);
    for (int r=0;r<4;r++) {
        x[4]^=rotl32(x[0]+x[12],7); x[8]^=rotl32(x[4]+x[0],9);
        x[12]^=rotl32(x[8]+x[4],13); x[0]^=rotl32(x[12]+x[8],18);
        x[9]^=rotl32(x[5]+x[1],7); x[13]^=rotl32(x[9]+x[5],9);
        x[1]^=rotl32(x[13]+x[9],13); x[5]^=rotl32(x[1]+x[13],18);
        x[14]^=rotl32(x[10]+x[6],7); x[2]^=rotl32(x[14]+x[10],9);
        x[6]^=rotl32(x[2]+x[14],13); x[10]^=rotl32(x[6]+x[2],18);
        x[3]^=rotl32(x[15]+x[11],7); x[7]^=rotl32(x[3]+x[15],9);
        x[11]^=rotl32(x[7]+x[3],13); x[15]^=rotl32(x[11]+x[7],18);
        x[1]^=rotl32(x[0]+x[3],7); x[2]^=rotl32(x[1]+x[0],9);
        x[3]^=rotl32(x[2]+x[1],13); x[0]^=rotl32(x[3]+x[2],18);
        x[6]^=rotl32(x[5]+x[4],7); x[7]^=rotl32(x[6]+x[5],9);
        x[4]^=rotl32(x[7]+x[6],13); x[5]^=rotl32(x[4]+x[7],18);
        x[11]^=rotl32(x[10]+x[9],7); x[8]^=rotl32(x[11]+x[10],9);
        x[9]^=rotl32(x[8]+x[11],13); x[10]^=rotl32(x[9]+x[8],18);
        x[12]^=rotl32(x[15]+x[14],7); x[13]^=rotl32(x[12]+x[15],9);
        x[14]^=rotl32(x[13]+x[12],13); x[15]^=rotl32(x[14]+x[13],18);
    }
    for (int i=0;i<16;i++) x[i]+=z[i];
}

__device__ __forceinline__ void salsa_shuffle_d(const uint32_t w[16], uint64_t d[8]) {
    d[0]=(uint64_t)w[0]|((uint64_t)w[5]<<32); d[1]=(uint64_t)w[10]|((uint64_t)w[15]<<32);
    d[2]=(uint64_t)w[4]|((uint64_t)w[9]<<32); d[3]=(uint64_t)w[14]|((uint64_t)w[3]<<32);
    d[4]=(uint64_t)w[8]|((uint64_t)w[13]<<32); d[5]=(uint64_t)w[2]|((uint64_t)w[7]<<32);
    d[6]=(uint64_t)w[12]|((uint64_t)w[1]<<32); d[7]=(uint64_t)w[6]|((uint64_t)w[11]<<32);
}
__device__ __forceinline__ void salsa_unshuffle_d(const uint64_t d[8], uint32_t w[16]) {
    w[0]=(uint32_t)d[0]; w[1]=(uint32_t)(d[6]>>32); w[2]=(uint32_t)d[5]; w[3]=(uint32_t)(d[3]>>32);
    w[4]=(uint32_t)d[2]; w[5]=(uint32_t)(d[0]>>32); w[6]=(uint32_t)d[7]; w[7]=(uint32_t)(d[5]>>32);
    w[8]=(uint32_t)d[4]; w[9]=(uint32_t)(d[2]>>32); w[10]=(uint32_t)d[1]; w[11]=(uint32_t)(d[7]>>32);
    w[12]=(uint32_t)d[6]; w[13]=(uint32_t)(d[4]>>32); w[14]=(uint32_t)d[3]; w[15]=(uint32_t)(d[1]>>32);
}
__device__ void load_block_d(const uint8_t *src, uint64_t blk[8]) {
    uint32_t w[16]; for (int i=0;i<16;i++) w[i]=le32dec_d(src+i*4);
    salsa_shuffle_d(w,blk);
}
__device__ void store_block_d(uint8_t *dst, const uint64_t blk[8]) {
    uint32_t w[16]; salsa_unshuffle_d(blk,w); for (int i=0;i<16;i++) le32enc_d(dst+i*4,w[i]);
}

/* --- pwxform --- */
__device__ void pwxform_round_d(uint64_t d[8], uint8_t *S0, uint8_t *S1) {
    for (int i=0;i<8;i+=2) {
        uint64_t x=d[i]&S_mask2; uint32_t lo=(uint32_t)x, hi=(uint32_t)(x>>32);
        uint64_t p00=*(uint64_t*)(S0+lo), p01=*(uint64_t*)(S0+lo+8);
        uint64_t p10=*(uint64_t*)(S1+hi), p11=*(uint64_t*)(S1+hi+8);
        uint64_t v=(((d[i]>>32)*(uint32_t)d[i]+p00)^p10)
                  |((((d[i+1]>>32)*(uint32_t)d[i+1]+p01)^p11)<<32);
        d[i]=(uint32_t)v; d[i+1]=v>>32;
    }
}
__device__ void pwxform_write4_d(uint64_t d[8], uint8_t *S0, uint8_t *S1, size_t *w) {
    for (int i=0;i<4;i+=2) {
        uint64_t x=d[i]&S_mask2; uint32_t lo=(uint32_t)x, hi=(uint32_t)(x>>32);
        uint64_t v=(((d[i]>>32)*(uint32_t)d[i]+*(uint64_t*)(S0+lo))^*(uint64_t*)(S1+hi))
                  |((((d[i+1]>>32)*(uint32_t)d[i+1]+*(uint64_t*)(S0+lo+8))^*(uint64_t*)(S1+hi+8))<<32);
        d[i]=(uint32_t)v; d[i+1]=v>>32;
        *(uint64_t*)(S0+*w)=d[i]; *(uint64_t*)(S0+*w+8)=d[i+1]; *w+=16;
    }
}
__device__ void pwxform_write2_d(uint64_t d[8], uint8_t *S0, uint8_t *S1, size_t *w) {
    for (int i=0;i<4;i+=2) {
        uint64_t x=d[i]&S_mask2; uint32_t lo=(uint32_t)x, hi=(uint32_t)(x>>32);
        uint64_t v=(((d[i]>>32)*(uint32_t)d[i]+*(uint64_t*)(S0+lo))^*(uint64_t*)(S1+hi))
                  |((((d[i+1]>>32)*(uint32_t)d[i+1]+*(uint64_t*)(S0+lo+8))^*(uint64_t*)(S1+hi+8))<<32);
        d[i]=(uint32_t)v; d[i+1]=v>>32;
        if (i<2) { *(uint64_t*)(S0+*w)=d[i]; *(uint64_t*)(S0+*w+8)=d[i+1]; *w+=16; }
    }
}
__device__ void pwxform_full_d(uint64_t d[8], uint8_t *S0, uint8_t *S1, uint8_t *S2, size_t *w) {
    for (int i=0;i<3;i++) pwxform_round_d(d,S0,S1);
    pwxform_write4_d(d,S0,S1,w);
    pwxform_write2_d(d,S0,S1,w);
    pwxform_write2_d(d,S0,S1,w);
    *w&=S_mask; uint8_t *t=S2; S2=S1; S1=S0; S0=t; (void)S0;
}

/* --- blockmix variants --- */
__device__ void blockmix_p2_d(uint64_t *X, int r, uint64_t *Bout, int boutIdx,
                              uint8_t *S0, uint8_t *S1, uint8_t *S2, size_t *w) {
    int rr=r*2-1; uint64_t x[8]; for (int i=0;i<8;i++) x[i]=X[rr*8+i];
    int i=0; for (;;) { for (int k=0;k<8;k++) x[k]^=X[i*8+k];
        pwxform_full_d(x,S0,S1,S2,w); if (i>=rr) break;
        for (int k=0;k<8;k++) Bout[boutIdx+i]=x[k]; i++; }
    uint32_t ws[16]; salsa_unshuffle_d(x,ws); salsa20_8_d(ws);
    uint64_t bx[8]; salsa_shuffle_d(ws,bx);
    for (int k=0;k<8;k++) Bout[boutIdx+i]=bx[k];
}

__device__ uint32_t blockmix_xor_p2_d(uint64_t *B1, int i1, const uint64_t *B2, int i2,
                                      uint64_t *Bout, int oi, int r,
                                      uint8_t *S0, uint8_t *S1, uint8_t *S2, size_t *w) {
    int rr=r*2-1; uint64_t x[8];
    for (int k=0;k<8;k++) x[k]=B1[(i1+rr)*8+k]^B2[(i2+rr)*8+k];
    int i=0; rr--;
    for (;;) { for (int k=0;k<8;k++) x[k]^=B1[(i1+i)*8+k]^B2[(i2+i)*8+k];
        pwxform_full_d(x,S0,S1,S2,w);
        for (int k=0;k<8;k++) Bout[(oi+i)*8+k]=x[k];
        for (int k=0;k<8;k++) x[k]^=B1[(i1+i+1)*8+k]^B2[(i2+i+1)*8+k];
        pwxform_full_d(x,S0,S1,S2,w);
        if (i>=rr) break;
        for (int k=0;k<8;k++) Bout[(oi+i+1)*8+k]=x[k]; i+=2; }
    i++; uint32_t ws[16]; salsa_unshuffle_d(x,ws); salsa20_8_d(ws);
    uint64_t bx[8]; salsa_shuffle_d(ws,bx);
    for (int k=0;k<8;k++) Bout[(oi+i)*8+k]=bx[k];
    return (uint32_t)bx[0];
}

__device__ uint32_t blockmix_xor_save_p2_d(uint64_t *B1out, int i1, const uint64_t *B2, int i2,
                                           int r, uint8_t *S0, uint8_t *S1, uint8_t *S2, size_t *w) {
    int rr=r*2-1; uint64_t x[8];
    for (int k=0;k<8;k++) x[k]=B1out[(i1+rr)*8+k]^B2[(i2+rr)*8+k];
    int i=0; rr--;
    for (;;) {
        uint64_t y[8]; for (int k=0;k<8;k++) y[k]=B1out[(i1+i)*8+k]^B2[(i2+i)*8+k];
        for (int k=0;k<8;k++) B1out[(i1+i)*8+k]=y[k];
        for (int k=0;k<8;k++) x[k]^=y[k];
        pwxform_full_d(x,S0,S1,S2,w);
        for (int k=0;k<8;k++) B1out[(i1+i)*8+k]=x[k];
        for (int k=0;k<8;k++) y[k]=B1out[(i1+i+1)*8+k]^B2[(i2+i+1)*8+k];
        for (int k=0;k<8;k++) B1out[(i1+i+1)*8+k]=y[k];
        for (int k=0;k<8;k++) x[k]^=y[k];
        pwxform_full_d(x,S0,S1,S2,w);
        if (i>=rr) break;
        for (int k=0;k<8;k++) B1out[(i1+i+1)*8+k]=x[k];
        i+=2; }
    i++; uint32_t ws[16]; salsa_unshuffle_d(x,ws); salsa20_8_d(ws);
    uint64_t bx[8]; salsa_shuffle_d(ws,bx);
    for (int k=0;k<8;k++) B1out[(i1+i)*8+k]=bx[k];
    return (uint32_t)bx[0];
}

/* --- smix1 / smix2 --- */
__device__ void smix1_p2_d(uint8_t *B, int r, uint32_t Nv,
                           uint64_t *V, uint64_t *XY,
                           uint8_t *S0, uint8_t *S1, uint8_t *S2, size_t *w,
                           int init_only) {
    int s=2*r; uint64_t *X=V, *Y=V+s, *V_j; uint32_t j,n;
    int limit=init_only?2:(2*r);
    for (uint32_t i=0;i<(uint32_t)limit;i++) {
        uint32_t tmp[16]; for (int k=0;k<16;k++) tmp[k]=le32dec_d(B+i*64+k*4);
        uint64_t blk[8]; salsa_shuffle_d(tmp,blk);
        for (int k=0;k<8;k++) X[i*8+k]=blk[k];
    }
    if (!init_only) for (uint32_t i=1;i<(uint32_t)r;i++)
        blockmix_p2_d(X+(i-1)*2,1,X,i*2,S0,S1,S2,w);
    blockmix_p2_d(X,r,Y,0,S0,S1,S2,w); X=Y+s;
    blockmix_p2_d(Y,r,X,0,S0,S1,S2,w); j=(uint32_t)X[0];
    for (n=2;n<Nv;n<<=1) {
        uint32_t m=(n<Nv/2)?n:(Nv-1-n);
        for (uint32_t i=1;i<m;i+=2) {
            Y=X+s; j&=n-1; j+=i-1; V_j=V+j*s;
            j=blockmix_xor_p2_d(X,0,V_j,0,Y,0,r,S0,S1,S2,w);
            j&=n-1; j+=i; V_j=V+j*s; X=Y+s;
            j=blockmix_xor_p2_d(Y,0,V_j,0,X,0,r,S0,S1,S2,w);
        }
    }
    n>>=1; j&=n-1; j+=Nv-2-n; V_j=V+j*s; Y=X+s;
    j=blockmix_xor_p2_d(X,0,V_j,0,Y,0,r,S0,S1,S2,w);
    j&=n-1; j+=Nv-1-n; V_j=V+j*s;
    blockmix_xor_p2_d(Y,0,V_j,0,XY,0,r,S0,S1,S2,w);
    for (uint32_t i=0;i<(uint32_t)(2*r);i++) {
        uint32_t tmp[16]; salsa_unshuffle_d(XY+i*8,tmp);
        for (int k=0;k<16;k++) le32enc_d(B+i*64+k*4,tmp[k]);
    }
}

__device__ void smix2_p2_d(uint8_t *B, int r, uint32_t Nv, uint32_t Nloop,
                           uint64_t *V, uint64_t *XY,
                           uint8_t *S0, uint8_t *S1, uint8_t *S2, size_t *w) {
    int s=2*r; uint64_t *X=XY; uint32_t j;
    for (uint32_t i=0;i<(uint32_t)(2*r);i++) {
        uint32_t tmp[16]; for (int k=0;k<16;k++) tmp[k]=le32dec_d(B+i*64+k*4);
        uint64_t blk[8]; salsa_shuffle_d(tmp,blk);
        for (int k=0;k<8;k++) X[i*8+k]=blk[k];
    }
    j=((uint32_t)X[0])&(Nv-1);
    do { uint64_t *V_j=V+j*s; j=blockmix_xor_save_p2_d(X,0,V_j,0,r,S0,S1,S2,w)&(Nv-1);
        V_j=V+j*s; j=blockmix_xor_save_p2_d(X,0,V_j,0,r,S0,S1,S2,w)&(Nv-1);
        Nloop-=2; } while (Nloop>0);
    for (uint32_t i=0;i<(uint32_t)(2*r);i++) {
        uint32_t tmp[16]; salsa_unshuffle_d(X+i*8,tmp);
        for (int k=0;k<16;k++) le32enc_d(B+i*64+k*4,tmp[k]);
    }
}

/* --- main yespower hash kernel --- */
__device__ void yespower_hash_d(const uint8_t *input, size_t inputlen,
                                const uint8_t *pers, size_t perslen,
                                uint8_t *scratch, uint8_t output[32]) {
    uint8_t *B=scratch, *Vb=scratch+B_SIZE, *XYb=scratch+B_SIZE+V_SIZE, *Sb=scratch+B_SIZE+V_SIZE+XY_SIZE;
    uint8_t *S0=Sb, *S1=Sb+Sbytes/3, *S2=Sb+2*Sbytes/3;
    size_t w=0;
    uint8_t sha256_digest[32];
    sha256_hash_d(input,inputlen,sha256_digest);
    uint8_t B128[128]; pbkdf2_sha256_1_d(sha256_digest,32,pers,perslen,B128,128);
    memcpy(B,B128,128);
    uint8_t sha256_save[32]; memcpy(sha256_save,B,32);
    uint64_t *V=(uint64_t*)Vb, *XY=(uint64_t*)XYb;
    uint32_t Nloop_rw=(N+2)/3; Nloop_rw++; Nloop_rw&=~(uint32_t)1;
    smix1_p2_d(B,1,Sbytes/128,V,XY,S0,S1,S2,&w,1);
    smix1_p2_d(B,R,N,V,XY,S0,S1,S2,&w,0);
    smix2_p2_d(B,R,N,Nloop_rw,V,XY,S0,S1,S2,&w);
    hmac_sha256_d(B+B_SIZE-64,64,sha256_save,32,output);
    (void)perslen;
}

/* --- global kernel (one thread per nonce) --- */
__global__ void yespower_kernel(
    const uint8_t *headers, uint8_t *outputs, int count,
    const uint8_t *pers, int perslen, uint8_t *scratch_pool)
{
    int idx = blockIdx.x * blockDim.x + threadIdx.x;
    if (idx >= count) return;
    uint8_t *my_scratch = scratch_pool + (size_t)idx * PER_THREAD;
    uint8_t my_output[32];
    yespower_hash_d(headers + (size_t)idx * 80, 80, pers, perslen, my_scratch, my_output);
    for (int i=0;i<32;i++) outputs[(size_t)idx*32+i]=my_output[i];
}

/* ================================================================== */
/*  Host bridge (extern "C")                                           */
/* ================================================================== */

static int gpu_dev_count = 0;
int gpu_max_batch = 0;
static uint8_t *d_scratch = NULL, *d_headers = NULL, *d_outputs = NULL, *d_pers = NULL;
static int pers_cap = 0;
static int initialized = 0;
static char gpu_last_error[256];

extern "C" {

static void set_last_error(cudaError_t err, const char *msg) {
    snprintf(gpu_last_error, sizeof(gpu_last_error), "%s: %s",
             msg, cudaGetErrorString(err));
}

static void set_last_error_str(const char *msg) {
    snprintf(gpu_last_error, sizeof(gpu_last_error), "%s", msg);
}

int gpu_init(void) {
    cudaError_t err;
    gpu_last_error[0] = '\0';

    err = cudaGetDeviceCount(&gpu_dev_count);
    if (err != cudaSuccess || gpu_dev_count < 1) {
        set_last_error(err, "cudaGetDeviceCount");
        return -1;
    }
    err = cudaSetDevice(0);
    if (err != cudaSuccess) {
        set_last_error(err, "cudaSetDevice");
        return -1;
    }

    size_t free_mem, total_mem;
    err = cudaMemGetInfo(&free_mem, &total_mem);
    if (err != cudaSuccess) {
        set_last_error(err, "cudaMemGetInfo");
        return -1;
    }

    size_t usable = free_mem * 8 / 10;
    gpu_max_batch = (int)(usable / PER_THREAD);
    if (gpu_max_batch < 1) gpu_max_batch = 1;
    if (gpu_max_batch > 256) gpu_max_batch = 256;

    size_t scratch_sz = (size_t)gpu_max_batch * PER_THREAD;
    err = cudaMalloc(&d_scratch, scratch_sz);
    if (err != cudaSuccess) { set_last_error(err, "cudaMalloc scratch"); goto fail; }
    err = cudaMalloc(&d_headers, (size_t)gpu_max_batch * 80);
    if (err != cudaSuccess) { set_last_error(err, "cudaMalloc headers"); goto fail; }
    err = cudaMalloc(&d_outputs, (size_t)gpu_max_batch * 32);
    if (err != cudaSuccess) { set_last_error(err, "cudaMalloc outputs"); goto fail; }

    {
        cudaDeviceProp prop;
        cudaGetDeviceProperties(&prop, 0);
        snprintf(gpu_last_error, sizeof(gpu_last_error),
                 "GPU %s  CC %d.%d  batch=%d  scratch=%zu MB",
                 prop.name, prop.major, prop.minor,
                 gpu_max_batch, scratch_sz / 1048576);
    }

    initialized = 1;
    return gpu_dev_count;
fail:
    if (d_scratch) { cudaFree(d_scratch); d_scratch = NULL; }
    if (d_headers) { cudaFree(d_headers); d_headers = NULL; }
    if (d_outputs) { cudaFree(d_outputs); d_outputs = NULL; }
    gpu_max_batch = 0;
    return -1;
}

int gpu_device_info(int device_id, char *name, size_t name_size, size_t *global_mem) {
    cudaDeviceProp prop;
    cudaError_t err = cudaGetDeviceProperties(&prop, device_id);
    if (err != cudaSuccess) return -1;
    strncpy(name, prop.name, name_size-1); name[name_size-1] = '\0';
    if (global_mem) *global_mem = prop.totalGlobalMem;
    return 0;
}

int gpu_hash(const uint8_t *headers, uint8_t *outputs, int count,
             const uint8_t *pers, int perslen) {
    if (!initialized) {
        set_last_error_str("not initialized");
        return -1;
    }
    if (count > gpu_max_batch) count = gpu_max_batch;
    if (count < 1) {
        set_last_error_str("count < 1");
        return -1;
    }
    cudaError_t err;

    err = cudaMemcpy(d_headers, headers, (size_t)count * 80, cudaMemcpyHostToDevice);
    if (err != cudaSuccess) { set_last_error(err, "cudaMemcpy H2D headers"); return -1; }

    if (perslen + 1 > pers_cap) {
        if (d_pers) cudaFree(d_pers);
        pers_cap = perslen + 1;
        err = cudaMalloc(&d_pers, pers_cap);
        if (err != cudaSuccess) { set_last_error(err, "cudaMalloc pers"); return -1; }
    }
    err = cudaMemcpy(d_pers, pers, perslen, cudaMemcpyHostToDevice);
    if (err != cudaSuccess) { set_last_error(err, "cudaMemcpy H2D pers"); return -1; }

    dim3 blockDim(256,1,1);
    dim3 gridDim((count+255)/256,1,1);
    yespower_kernel<<<gridDim,blockDim>>>(d_headers, d_outputs, count, d_pers, perslen, d_scratch);

    err = cudaGetLastError();
    if (err != cudaSuccess) { set_last_error(err, "kernel launch"); return -1; }

    err = cudaDeviceSynchronize();
    if (err != cudaSuccess) { set_last_error(err, "kernel execution"); return -1; }

    err = cudaMemcpy(outputs, d_outputs, (size_t)count * 32, cudaMemcpyDeviceToHost);
    if (err != cudaSuccess) { set_last_error(err, "cudaMemcpy D2H outputs"); return -1; }
    return 0;
}

void gpu_close(void) {
    if (d_scratch) { cudaFree(d_scratch); d_scratch = NULL; }
    if (d_headers) { cudaFree(d_headers); d_headers = NULL; }
    if (d_outputs) { cudaFree(d_outputs); d_outputs = NULL; }
    if (d_pers)    { cudaFree(d_pers);    d_pers = NULL; }
    pers_cap = 0; initialized = 0; gpu_max_batch = 0;
}

int gpu_reset(void) {
    gpu_close();
    cudaDeviceReset();
    gpu_last_error[0] = '\0';
    return gpu_init();
}

const char *gpu_error_string(void) {
    return gpu_last_error;
}

} /* extern "C" */
