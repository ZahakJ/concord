package audio

import "errors"

// Reed–Solomon over GF(2^8) (primitive polynomial 0x11d, generator α = 2,
// first consecutive root α^0), the textbook errors-only decoder:
// syndromes → Berlekamp–Massey → Chien search → Forney. A codeword of n bytes
// with nsym parity bytes corrects up to nsym/2 wrong bytes anywhere in it.
// Anything worse is reported as an error, and the frame CRC behind it catches
// the rare miscorrection that slips through as a "valid" codeword.

var errUncorrectable = errors.New("audio: too many byte errors to correct")

var (
	gfExp [512]byte
	gfLog [256]int
)

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		gfExp[i] = byte(x)
		gfLog[x] = i
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11d
		}
	}
	for i := 255; i < 512; i++ {
		gfExp[i] = gfExp[i-255]
	}
}

func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[gfLog[a]+gfLog[b]]
}

func gfDiv(a, b byte) byte {
	if a == 0 {
		return 0
	}
	return gfExp[(gfLog[a]+255-gfLog[b])%255]
}

// gfPow2 is α^n for any integer n.
func gfPow2(n int) byte { return gfExp[((n%255)+255)%255] }

func gfInv(a byte) byte { return gfExp[255-gfLog[a]] }

// Polynomials are byte slices, highest-degree coefficient first.

func polyScale(p []byte, x byte) []byte {
	out := make([]byte, len(p))
	for i, c := range p {
		out[i] = gfMul(c, x)
	}
	return out
}

func polyAdd(p, q []byte) []byte {
	n := max(len(p), len(q))
	out := make([]byte, n)
	copy(out[n-len(p):], p)
	for i, c := range q {
		out[n-len(q)+i] ^= c
	}
	return out
}

func polyMul(p, q []byte) []byte {
	out := make([]byte, len(p)+len(q)-1)
	for j, b := range q {
		for i, a := range p {
			out[i+j] ^= gfMul(a, b)
		}
	}
	return out
}

func polyEval(p []byte, x byte) byte {
	y := p[0]
	for _, c := range p[1:] {
		y = gfMul(y, x) ^ c
	}
	return y
}

func rsGenerator(nsym int) []byte {
	g := []byte{1}
	for i := 0; i < nsym; i++ {
		g = polyMul(g, []byte{1, gfPow2(i)})
	}
	return g
}

// rsEncode returns msg followed by nsym parity bytes.
func rsEncode(msg []byte, nsym int) []byte {
	gen := rsGenerator(nsym)
	out := make([]byte, len(msg)+nsym)
	copy(out, msg)
	for i := range msg {
		c := out[i]
		if c == 0 {
			continue
		}
		for j := 1; j < len(gen); j++ {
			out[i+j] ^= gfMul(gen[j], c)
		}
	}
	copy(out, msg)
	return out
}

// syndromes returns [0, S0, S1, … S(nsym-1)]; the leading zero keeps the
// indexing of the classic formulation below.
func syndromes(cw []byte, nsym int) []byte {
	s := make([]byte, nsym+1)
	for i := 0; i < nsym; i++ {
		s[i+1] = polyEval(cw, gfPow2(i))
	}
	return s
}

// rsDecode corrects cw (data ‖ nsym parity) in place and returns the data
// part, or errUncorrectable.
func rsDecode(cw []byte, nsym int) ([]byte, error) {
	if len(cw) > 255 || len(cw) <= nsym {
		return nil, errUncorrectable
	}
	synd := syndromes(cw, nsym)
	clean := true
	for _, s := range synd {
		if s != 0 {
			clean = false
			break
		}
	}
	if clean {
		return cw[:len(cw)-nsym], nil
	}

	// Berlekamp–Massey: the error locator polynomial.
	errLoc, oldLoc := []byte{1}, []byte{1}
	for i := 0; i < nsym; i++ {
		k := i + 1
		delta := synd[k]
		for j := 1; j < len(errLoc); j++ {
			delta ^= gfMul(errLoc[len(errLoc)-1-j], synd[k-j])
		}
		oldLoc = append(oldLoc, 0)
		if delta != 0 {
			if len(oldLoc) > len(errLoc) {
				newLoc := polyScale(oldLoc, delta)
				oldLoc = polyScale(errLoc, gfInv(delta))
				errLoc = newLoc
			}
			errLoc = polyAdd(errLoc, polyScale(oldLoc, delta))
		}
	}
	for len(errLoc) > 0 && errLoc[0] == 0 {
		errLoc = errLoc[1:]
	}
	nerr := len(errLoc) - 1
	if nerr <= 0 || nerr*2 > nsym {
		return nil, errUncorrectable
	}

	// Chien search: where the errors are.
	rev := make([]byte, len(errLoc))
	for i, c := range errLoc {
		rev[len(errLoc)-1-i] = c
	}
	var pos []int
	for i := 0; i < len(cw); i++ {
		if polyEval(rev, gfPow2(i)) == 0 {
			pos = append(pos, len(cw)-1-i)
		}
	}
	if len(pos) != nerr {
		return nil, errUncorrectable
	}

	// Forney: how big each error is.
	coef := make([]int, len(pos))
	for i, p := range pos {
		coef[i] = len(cw) - 1 - p
	}
	loc := []byte{1}
	for _, c := range coef {
		loc = polyMul(loc, polyAdd([]byte{1}, []byte{gfPow2(c), 0}))
	}
	rsynd := make([]byte, len(synd))
	for i, s := range synd {
		rsynd[len(synd)-1-i] = s
	}
	prod := polyMul(rsynd, loc)
	keep := len(loc) // remainder modulo x^(len(loc)) is the low-order terms
	if keep > len(prod) {
		keep = len(prod)
	}
	eval := prod[len(prod)-keep:]
	// eval is used reversed-then-reversed in the reference; evaluate its
	// low-order-first form directly.
	X := make([]byte, len(coef))
	for i, c := range coef {
		X[i] = gfPow2(-(255 - c))
	}
	for i, xi := range X {
		xiInv := gfInv(xi)
		prime := byte(1)
		for j, xj := range X {
			if j != i {
				prime = gfMul(prime, 1^gfMul(xiInv, xj))
			}
		}
		if prime == 0 {
			return nil, errUncorrectable
		}
		y := gfMul(xi, polyEval(eval, xiInv))
		cw[pos[i]] ^= gfDiv(y, prime)
	}
	for _, s := range syndromes(cw, nsym) {
		if s != 0 {
			return nil, errUncorrectable
		}
	}
	return cw[:len(cw)-nsym], nil
}
