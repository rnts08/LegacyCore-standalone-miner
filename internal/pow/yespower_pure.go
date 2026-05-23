package pow

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"

	"github.com/legacycoin/standalone-miner/internal/chainhash"
)

const (
	yespowerN        = 2048
	yespowerR        = 32
	sWidth10         = 11
	pwxSimple        = 2
	sBytes1          = (1 << sWidth10) * pwxSimple * 8
	sMask            = ((1 << sWidth10) - 1) * pwxSimple * 8
	sMask2    uint64 = uint64(sMask)<<32 | uint64(sMask)
)

type ypBlock [64]byte

type ypCtx struct {
	mem        []ypBlock
	s0, s1, s2 []ypBlock
	w          int
}

func yespowerHash(input []byte, pers string) chainhash.Hash {
	srcHash := sha256.Sum256(input)
	bSize := 128 * yespowerR
	b := pbkdf2SHA256One(srcHash[:], []byte(pers), bSize)
	seedHash := make([]byte, 32)
	copy(seedHash, b[:32])

	v := make([]ypBlock, yespowerN*2*yespowerR)
	xy := make([]ypBlock, 2*yespowerR+1)
	smem := make([]ypBlock, 3*sBytes1/64)
	ctx := &ypCtx{
		mem: smem,
		s0:  smem[:sBytes1/64],
		s1:  smem[sBytes1/64 : 2*sBytes1/64],
		s2:  smem[2*sBytes1/64:],
	}

	// yespower 1.0 first fills the pwxform S-box memory via an SMix call
	// without pwxform, then runs the main SMix with pwxform enabled.
	smix1(b, 1, len(smem)/2, smem, xy, nil)
	smix1(b, yespowerR, yespowerN, v, xy, ctx)
	nLoopRW := ((yespowerN + 2) / 3) + 1
	nLoopRW &^= 1
	smix2(b, yespowerR, yespowerN, nLoopRW, v, xy, ctx)

	var out chainhash.Hash
	mac := hmac.New(sha256.New, b[bSize-64:bSize])
	_, _ = mac.Write(seedHash)
	copy(out[:], mac.Sum(nil))
	return out
}

func pbkdf2SHA256One(password, salt []byte, keyLen int) []byte {
	out := make([]byte, 0, keyLen)
	var counter [4]byte
	for block := uint32(1); len(out) < keyLen; block++ {
		binary.BigEndian.PutUint32(counter[:], block)
		mac := hmac.New(sha256.New, password)
		_, _ = mac.Write(salt)
		_, _ = mac.Write(counter[:])
		out = append(out, mac.Sum(nil)...)
	}
	return out[:keyLen]
}

func smix1(b []byte, r, n int, v, xy []ypBlock, ctx *ypCtx) {
	s := 2 * r
	x := 0
	y := s

	limit := 2
	if ctx == nil {
		limit = 2 * r
	}
	for i := 0; i < limit; i++ {
		var tmp ypBlock
		for k := 0; k < 16; k++ {
			setW(&tmp, k, binary.LittleEndian.Uint32(b[i*64+k*4:]))
		}
		salsaShuffle(&tmp, &v[x+i])
	}

	if ctx != nil {
		for i := 1; i < r; i++ {
			blockmix(v, x+(i-1)*2, v, x+i*2, 1, ctx)
		}
	}

	blockmix(v, x, v, y, r, ctx)
	x = y + s
	blockmix(v, y, v, x, r, ctx)
	j := integerify(v, x, r)

	for nn := 2; nn < n; nn <<= 1 {
		m := nn
		if nn >= n/2 {
			m = n - 1 - nn
		}
		for i := 1; i < m; i += 2 {
			y = x + s
			j &= uint32(nn - 1)
			j += uint32(i - 1)
			vj := int(j) * s
			j = blockmixXor(v, x, v, vj, v, y, r, ctx)
			j &= uint32(nn - 1)
			j += uint32(i)
			vj = int(j) * s
			x = y + s
			j = blockmixXor(v, y, v, vj, v, x, r, ctx)
		}
	}
	nn := n >> 1

	j &= uint32(nn - 1)
	j += uint32(n - 2 - nn)
	vj := int(j) * s
	_ = blockmixXor(v, x, v, vj, xy, 0, r, ctx)
	j &= uint32(nn - 1)
	j += uint32(n - 1 - nn)
	vj = int(j) * s
	blockmixXor(xy, 0, v, vj, xy, 0, r, ctx)

	for i := 0; i < 2*r; i++ {
		var tmp ypBlock
		for k := 0; k < 16; k++ {
			setW(&tmp, k, getW(&xy[i], k))
		}
		var dst ypBlock
		salsaUnshuffle(&tmp, &dst)
		copy(b[i*64:(i+1)*64], dst[:])
	}
}

func smix2(b []byte, r, n, nLoop int, v, xy []ypBlock, ctx *ypCtx) {
	s := 2 * r
	x := 0
	y := s
	for i := 0; i < 2*r; i++ {
		var tmp ypBlock
		for k := 0; k < 16; k++ {
			setW(&tmp, k, binary.LittleEndian.Uint32(b[i*64+k*4:]))
		}
		salsaShuffle(&tmp, &xy[x+i])
	}

	j := integerify(xy, x, r) & uint32(n-1)
	for nLoop > 0 {
		vj := int(j) * s
		j = blockmixXorSave(xy, x, v, vj, r, ctx) & uint32(n-1)
		vj = int(j) * s
		j = blockmixXorSave(xy, x, v, vj, r, ctx) & uint32(n-1)
		nLoop -= 2
	}

	for i := 0; i < 2*r; i++ {
		var tmp ypBlock
		for k := 0; k < 16; k++ {
			setW(&tmp, k, getW(&xy[x+i], k))
		}
		var dst ypBlock
		salsaUnshuffle(&tmp, &dst)
		copy(b[i*64:(i+1)*64], dst[:])
	}
	_ = y
}

func blockmix(bin []ypBlock, binIdx int, bout []ypBlock, boutIdx int, r int, ctx *ypCtx) {
	if ctx == nil {
		blockmixSalsa(bin, binIdx, bout, boutIdx)
		return
	}
	rr := r*2 - 1
	x := bin[binIdx+rr]
	for i := 0; ; i++ {
		xorBlock(&x, &bin[binIdx+i])
		pwxform(&x, ctx)
		if i >= rr {
			salsa20(&x, &bout[boutIdx+i], 1)
			return
		}
		bout[boutIdx+i] = x
	}
}

func blockmixXor(bin1 []ypBlock, idx1 int, bin2 []ypBlock, idx2 int, bout []ypBlock, outIdx int, r int, ctx *ypCtx) uint32 {
	if ctx == nil {
		return blockmixSalsaXor(bin1, idx1, bin2, idx2, bout, outIdx)
	}
	rr := r*2 - 1
	x := xorBlocks(&bin1[idx1+rr], &bin2[idx2+rr])
	rr--
	i := 0
	for {
		xorBlock(&x, &bin1[idx1+i])
		xorBlock(&x, &bin2[idx2+i])
		pwxform(&x, ctx)
		bout[outIdx+i] = x

		xorBlock(&x, &bin1[idx1+i+1])
		xorBlock(&x, &bin2[idx2+i+1])
		pwxform(&x, ctx)

		if i >= rr {
			break
		}
		bout[outIdx+i+1] = x
		i += 2
	}
	i++
	salsa20(&x, &bout[outIdx+i], 1)
	return getW(&x, 0)
}

func blockmixXorSave(binout []ypBlock, idx1 int, bin2 []ypBlock, idx2 int, r int, ctx *ypCtx) uint32 {
	rr := r*2 - 1
	x := xorBlocks(&binout[idx1+rr], &bin2[idx2+rr])
	rr--
	i := 0
	for {
		y := xorBlocks(&binout[idx1+i], &bin2[idx2+i])
		binout[idx1+i] = y
		xorBlock(&x, &y)
		pwxform(&x, ctx)
		binout[idx1+i] = x

		y = xorBlocks(&binout[idx1+i+1], &bin2[idx2+i+1])
		binout[idx1+i+1] = y
		xorBlock(&x, &y)
		pwxform(&x, ctx)

		if i >= rr {
			break
		}
		binout[idx1+i+1] = x
		i += 2
	}
	i++
	salsa20(&x, &binout[idx1+i], 1)
	return getW(&x, 0)
}

func blockmixSalsa(bin []ypBlock, binIdx int, bout []ypBlock, outIdx int) {
	x := bin[binIdx+1]
	xorBlock(&x, &bin[binIdx])
	salsa20(&x, &bout[outIdx], 1)
	xorBlock(&x, &bin[binIdx+1])
	salsa20(&x, &bout[outIdx+1], 1)
}

func blockmixSalsaXor(bin1 []ypBlock, idx1 int, bin2 []ypBlock, idx2 int, bout []ypBlock, outIdx int) uint32 {
	x := xorBlocks(&bin1[idx1+1], &bin2[idx2+1])
	xorBlock(&x, &bin1[idx1])
	xorBlock(&x, &bin2[idx2])
	salsa20(&x, &bout[outIdx], 1)
	xorBlock(&x, &bin1[idx1+1])
	xorBlock(&x, &bin2[idx2+1])
	salsa20(&x, &bout[outIdx+1], 1)
	return getW(&x, 0)
}

func pwxform(x *ypBlock, ctx *ypCtx) {
	for i := 0; i < 4; i++ {
		pwxPair(x, i*2, ctx, nil)
	}
	for i := 0; i < 4; i++ {
		pwxPair(x, i*2, ctx, nil)
	}
	for i := 0; i < 4; i++ {
		pwxPair(x, i*2, ctx, nil)
	}
	pwxRoundWrite4(x, ctx)
	pwxRoundWrite2(x, ctx)
	pwxRoundWrite2(x, ctx)
	ctx.w &= sMask
	ctx.s0, ctx.s1, ctx.s2 = ctx.s2, ctx.s0, ctx.s1
}

func pwxRoundWrite4(x *ypBlock, ctx *ypCtx) {
	pwxPair(x, 0, ctx, ctx.s0)
	pwxPair(x, 2, ctx, ctx.s1)
	ctx.w += 16
	pwxPair(x, 4, ctx, ctx.s0)
	pwxPair(x, 6, ctx, ctx.s1)
	ctx.w += 16
}

func pwxRoundWrite2(x *ypBlock, ctx *ypCtx) {
	pwxPair(x, 0, ctx, ctx.s0)
	pwxPair(x, 2, ctx, ctx.s1)
	ctx.w += 16
	pwxPair(x, 4, ctx, nil)
	pwxPair(x, 6, ctx, nil)
}

func pwxPair(x *ypBlock, off int, ctx *ypCtx, writeTo []ypBlock) {
	x0 := getD(x, off)
	x1 := getD(x, off+1)
	idx := x0 & sMask2
	// Safe conversion: idx is masked to sMask2, ensuring values fit in valid range
	lo := int(uint32(idx & 0xffffffff))
	hi := int(uint32((idx >> 32) & 0xffffffff))
	p00 := getDAt(ctx.s0, lo)
	p01 := getDAt(ctx.s0, lo+8)
	p10 := getDAt(ctx.s1, hi)
	p11 := getDAt(ctx.s1, hi+8)
	// Safe conversion: masking to lower 32 bits before conversion
	x0 = ((x0>>32)*uint64(uint32(x0&0xffffffff)) + p00) ^ p10
	x1 = ((x1>>32)*uint64(uint32(x1&0xffffffff)) + p01) ^ p11
	setD(x, off, x0)
	setD(x, off+1, x1)
	if writeTo != nil {
		setDAt(writeTo, ctx.w, x0)
		setDAt(writeTo, ctx.w+8, x1)
	}
}

func salsa20(b *ypBlock, bout *ypBlock, doubleRounds uint32) {
	var x ypBlock
	salsaUnshuffle(b, &x)
	for ; doubleRounds > 0; doubleRounds-- {
		qr := func(a, b, c, d int) {
			setW(&x, b, getW(&x, b)^rotl32(getW(&x, a)+getW(&x, d), 7))
			setW(&x, c, getW(&x, c)^rotl32(getW(&x, b)+getW(&x, a), 9))
			setW(&x, d, getW(&x, d)^rotl32(getW(&x, c)+getW(&x, b), 13))
			setW(&x, a, getW(&x, a)^rotl32(getW(&x, d)+getW(&x, c), 18))
		}
		qr(0, 4, 8, 12)
		qr(5, 9, 13, 1)
		qr(10, 14, 2, 6)
		qr(15, 3, 7, 11)
		qr(0, 1, 2, 3)
		qr(5, 6, 7, 4)
		qr(10, 11, 8, 9)
		qr(15, 12, 13, 14)
	}
	salsaShuffle(&x, bout)
	for i := 0; i < 16; i++ {
		v := getW(bout, i) + getW(b, i)
		setW(bout, i, v)
		setW(b, i, v)
	}
}

func salsaShuffle(in *ypBlock, out *ypBlock) {
	combine := func(o, i1, i2 int) {
		setD(out, o, uint64(getW(in, i1*2))|uint64(getW(in, i2*2+1))<<32)
	}
	combine(0, 0, 2)
	combine(1, 5, 7)
	combine(2, 2, 4)
	combine(3, 7, 1)
	combine(4, 4, 6)
	combine(5, 1, 3)
	combine(6, 6, 0)
	combine(7, 3, 5)
}

func salsaUnshuffle(in *ypBlock, out *ypBlock) {
	uncombine := func(o, i1, i2 int) {
		// Safe conversion: masking to lower 32 bits before conversion
		setW(out, o*2, uint32(getD(in, i1)&0xffffffff))
		setW(out, o*2+1, uint32((getD(in, i2)>>32)&0xffffffff))
	}
	uncombine(0, 0, 6)
	uncombine(1, 5, 3)
	uncombine(2, 2, 0)
	uncombine(3, 7, 5)
	uncombine(4, 4, 2)
	uncombine(5, 1, 7)
	uncombine(6, 6, 4)
	uncombine(7, 3, 1)
}

func integerify(blocks []ypBlock, idx int, r int) uint32 {
	// Safe conversion: lower 32 bits of uint64
	return uint32(getD(&blocks[idx+2*r-1], 0) & 0xffffffff)
}

func xorBlocks(a, b *ypBlock) ypBlock {
	var out ypBlock
	for i := 0; i < 8; i++ {
		setD(&out, i, getD(a, i)^getD(b, i))
	}
	return out
}

func xorBlock(a, b *ypBlock) {
	for i := 0; i < 8; i++ {
		setD(a, i, getD(a, i)^getD(b, i))
	}
}

func getW(b *ypBlock, i int) uint32 {
	return binary.LittleEndian.Uint32(b[i*4:])
}

func setW(b *ypBlock, i int, v uint32) {
	binary.LittleEndian.PutUint32(b[i*4:], v)
}

func getD(b *ypBlock, i int) uint64 {
	return binary.LittleEndian.Uint64(b[i*8:])
}

func setD(b *ypBlock, i int, v uint64) {
	binary.LittleEndian.PutUint64(b[i*8:], v)
}

func getDAt(blocks []ypBlock, off int) uint64 {
	return binary.LittleEndian.Uint64(blocks[off/64][off%64:])
}

func setDAt(blocks []ypBlock, off int, v uint64) {
	binary.LittleEndian.PutUint64(blocks[off/64][off%64:], v)
}

func rotl32(x uint32, n uint) uint32 {
	return x<<n | x>>(32-n)
}
