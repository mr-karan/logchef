package reports

import (
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/mr-karan/logchef/pkg/models"
)

// This file ports frontend/src/utils/exactNumber.ts and numberFormat.ts, so
// a stat tile in an email prints the digits the dashboard prints. The fixture
// pkg/models/testdata/report_number_cases.json feeds the tests of both.

// exact is coefficient × 10^-scale with no trailing zeros in the
// coefficient. Zero is 0 × 10^0.
type exact struct {
	coef  *big.Int
	scale int
}

var (
	bigTen = big.NewInt(10)
)

func normalizeExact(coef *big.Int, scale int) exact {
	c := new(big.Int).Set(coef)
	if c.Sign() == 0 {
		return exact{coef: c, scale: 0}
	}
	q, r := new(big.Int), new(big.Int)
	for {
		q.QuoRem(c, bigTen, r)
		if r.Sign() != 0 {
			return exact{coef: c, scale: scale}
		}
		c.Set(q)
		scale--
	}
}

func parseExact(literal string) (exact, bool) {
	coef, scale, ok := models.ParseExactDecimal(literal)
	if !ok {
		return exact{}, false
	}
	return exact{coef: coef, scale: scale}, true
}

// floatExact is the exact value of a float's shortest decimal form, as
// parseExact(number) reads String(number) in the frontend.
func floatExact(f float64) (exact, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return exact{}, false
	}
	return parseExact(strconv.FormatFloat(f, 'g', -1, 64))
}

func pow10(n int) *big.Int {
	return new(big.Int).Exp(bigTen, big.NewInt(int64(n)), nil)
}

func (a exact) add(b exact) exact {
	scale := max(a.scale, b.scale)
	left := new(big.Int).Mul(a.coef, pow10(scale-a.scale))
	right := new(big.Int).Mul(b.coef, pow10(scale-b.scale))
	return normalizeExact(left.Add(left, right), scale)
}

func (a exact) cmp(b exact) int {
	return a.add(exact{coef: new(big.Int).Neg(b.coef), scale: b.scale}).coef.Sign()
}

func (a exact) mul(b exact) exact {
	return normalizeExact(new(big.Int).Mul(a.coef, b.coef), a.scale+b.scale)
}

func (a exact) abs() exact {
	return exact{coef: new(big.Int).Abs(a.coef), scale: a.scale}
}

// round rounds half away from zero to decimals places.
func (a exact) round(decimals int) exact {
	if a.scale <= decimals {
		return a
	}
	return normalizeExact(roundDivide(a.coef, pow10(a.scale-decimals)), decimals)
}

func roundDivide(value, divisor *big.Int) *big.Int {
	q, r := new(big.Int).QuoRem(new(big.Int).Abs(value), divisor, new(big.Int))
	if r.Mul(r, big.NewInt(2)).Cmp(divisor) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if value.Sign() < 0 {
		q.Neg(q)
	}
	return q
}

// average divides a sum by count, rounded to 10 decimal places.
func (a exact) average(count int64) exact {
	shift := 10 - a.scale
	numerator := new(big.Int).Set(a.coef)
	denominator := big.NewInt(count)
	if shift >= 0 {
		numerator.Mul(numerator, pow10(shift))
	} else {
		denominator.Mul(denominator, pow10(-shift))
	}
	return normalizeExact(roundDivide(numerator, denominator), 10)
}

// text prints the digits, rounded to and padded to decimals when it is not
// negative.
func (a exact) text(decimals int) string {
	rounded := a
	if decimals >= 0 {
		rounded = a.round(decimals)
	}
	digits := new(big.Int).Abs(rounded.coef).String()
	point := len(digits) - rounded.scale
	var whole, fraction string
	switch {
	case point <= 0:
		whole, fraction = "0", strings.Repeat("0", -point)+digits
	case point >= len(digits):
		whole = digits + strings.Repeat("0", point-len(digits))
	default:
		whole, fraction = digits[:point], digits[point:]
	}
	if decimals > len(fraction) {
		fraction += strings.Repeat("0", decimals-len(fraction))
	}
	sign := ""
	if rounded.coef.Sign() < 0 {
		sign = "-"
	}
	if fraction == "" {
		return sign + whole
	}
	return sign + whole + "." + fraction
}

// Number is a frame number: exact digits, or a float64 from a Float column.
type Number struct {
	exact   exact
	float   float64
	isFloat bool
}

func exactNumber(e exact) Number   { return Number{exact: e} }
func floatNumber(f float64) Number { return Number{float: f, isFloat: true} }

// FormatNumber prints n with the stat format of RFC 0003 in the en-US
// locale, as formatNumber does in the frontend without compact fitting.
func FormatNumber(n Number, f models.DashboardNumberFormat) string {
	scale := 1.0
	if f.Scale != nil {
		scale = *f.Scale
	}
	var (
		scaled exact
		ok     bool
	)
	if n.isFloat {
		scaled, ok = floatExact(n.float * scale)
	} else if factor, factorOK := floatExact(scale); factorOK {
		scaled, ok = n.exact.mul(factor), true
	}
	if !ok {
		return "Overflow"
	}
	unit := map[string]string{"bytes": " B", "duration_ms": " ms", "duration_s": " s", "percent": "%"}[f.Unit]
	decimals := 2
	if f.Decimals != nil {
		decimals = *f.Decimals
	}
	atLeast := func(v exact, bound int64) bool {
		return v.round(decimals).abs().cmp(exact{coef: big.NewInt(bound)}) >= 0
	}
	switch {
	case f.Unit == "bytes":
		units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
		i := 0
		for i < len(units)-1 && atLeast(scaled, 1024) {
			scaled = scaled.average(1024)
			i++
		}
		unit = " " + units[i]
	case f.Unit == "duration_ms" || f.Unit == "duration_s":
		if f.Unit == "duration_s" {
			scaled = scaled.mul(exact{coef: big.NewInt(1000)})
		}
		units := []string{"ms", "s", "min", "h", "d"}
		divisors := []int64{1000, 60, 60, 24}
		i := 0
		for i < len(divisors) && atLeast(scaled, divisors[i]) {
			scaled = scaled.average(divisors[i])
			i++
		}
		unit = " " + units[i]
	case f.Notation == "compact":
		suffixes := []string{"", "K", "M", "B", "T", "Q"}
		i := 0
		for i < len(suffixes)-1 && atLeast(scaled, 1000) {
			scaled = scaled.mul(exact{coef: big.NewInt(1), scale: 3})
			i++
		}
		unit = suffixes[i] + unit
	}
	padTo := -1
	if f.Decimals != nil {
		padTo = *f.Decimals
	}
	grouping := f.Grouping == nil || *f.Grouping
	return f.Prefix + groupDigits(scaled.round(decimals).text(padTo), grouping) + unit + f.Suffix
}

// groupDigits puts en-US thousands separators into the whole part of a
// decimal string.
func groupDigits(s string, grouping bool) string {
	if !grouping {
		return s
	}
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	whole, fraction, hasFraction := strings.Cut(s, ".")
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if hasFraction {
		return sign + b.String() + "." + fraction
	}
	return sign + b.String()
}
