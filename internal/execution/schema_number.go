package execution

import (
	"encoding/json"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// decimalNumber stores only the supplied significant digits and the exponent;
// even extreme exponents never expand into a string or integer of that size.
type decimalNumber struct {
	negative bool
	digits   string
	scale    *big.Int
}

func numberValue(value any) (decimalNumber, bool) {
	var text string
	switch n := value.(type) {
	case json.Number:
		text = string(n)
	case float32:
		if math.IsNaN(float64(n)) || math.IsInf(float64(n), 0) {
			return decimalNumber{}, false
		}
		text = strconv.FormatFloat(float64(n), 'g', -1, 32)
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return decimalNumber{}, false
		}
		text = strconv.FormatFloat(n, 'g', -1, 64)
	case int:
		text = strconv.FormatInt(int64(n), 10)
	case int8:
		text = strconv.FormatInt(int64(n), 10)
	case int16:
		text = strconv.FormatInt(int64(n), 10)
	case int32:
		text = strconv.FormatInt(int64(n), 10)
	case int64:
		text = strconv.FormatInt(n, 10)
	case uint:
		text = strconv.FormatUint(uint64(n), 10)
	case uint8:
		text = strconv.FormatUint(uint64(n), 10)
	case uint16:
		text = strconv.FormatUint(uint64(n), 10)
	case uint32:
		text = strconv.FormatUint(uint64(n), 10)
	case uint64:
		text = strconv.FormatUint(n, 10)
	default:
		return decimalNumber{}, false
	}
	return parseDecimalNumber(text)
}

func parseDecimalNumber(text string) (decimalNumber, bool) {
	out := decimalNumber{scale: new(big.Int)}
	if strings.HasPrefix(text, "-") {
		out.negative = true
		text = text[1:]
	}
	if text == "" {
		return decimalNumber{}, false
	}
	index := 0
	if text[index] == '0' {
		index++
		if index < len(text) && text[index] >= '0' && text[index] <= '9' {
			return decimalNumber{}, false
		}
	} else {
		if text[index] < '1' || text[index] > '9' {
			return decimalNumber{}, false
		}
		for index < len(text) && text[index] >= '0' && text[index] <= '9' {
			index++
		}
	}
	integerPart := text[:index]
	fraction := ""
	if index < len(text) && text[index] == '.' {
		index++
		start := index
		for index < len(text) && text[index] >= '0' && text[index] <= '9' {
			index++
		}
		if start == index {
			return decimalNumber{}, false
		}
		fraction = text[start:index]
	}
	if index < len(text) && (text[index] == 'e' || text[index] == 'E') {
		index++
		negativeExponent := false
		if index < len(text) && (text[index] == '+' || text[index] == '-') {
			negativeExponent = text[index] == '-'
			index++
		}
		start := index
		for index < len(text) && text[index] >= '0' && text[index] <= '9' {
			index++
		}
		if start == index {
			return decimalNumber{}, false
		}
		if _, ok := out.scale.SetString(text[start:index], 10); !ok {
			return decimalNumber{}, false
		}
		if negativeExponent {
			out.scale.Neg(out.scale)
		}
	}
	if index != len(text) {
		return decimalNumber{}, false
	}
	out.digits = strings.TrimLeft(integerPart+fraction, "0")
	if out.digits == "" {
		out.negative = false
		out.digits = "0"
		out.scale.SetInt64(0)
		return out, true
	}
	trimmed := strings.TrimRight(out.digits, "0")
	out.scale.Sub(out.scale, big.NewInt(int64(len(fraction))))
	out.scale.Add(out.scale, big.NewInt(int64(len(out.digits)-len(trimmed))))
	out.digits = trimmed
	return out, true
}

func (n decimalNumber) integer() bool {
	return n.digits == "0" || n.scale.Sign() >= 0
}

func compareDecimalNumbers(a, b decimalNumber) int {
	if a.digits == "0" && b.digits == "0" {
		return 0
	}
	if a.negative != b.negative {
		if a.negative {
			return -1
		}
		return 1
	}
	if a.digits == "0" {
		if b.negative {
			return 1
		}
		return -1
	}
	if b.digits == "0" {
		if a.negative {
			return -1
		}
		return 1
	}
	orderA := new(big.Int).Add(a.scale, big.NewInt(int64(len(a.digits))))
	orderB := new(big.Int).Add(b.scale, big.NewInt(int64(len(b.digits))))
	cmp := orderA.Cmp(orderB)
	if cmp == 0 {
		length := len(a.digits)
		if len(b.digits) > length {
			length = len(b.digits)
		}
		for i := 0; i < length; i++ {
			da, db := byte('0'), byte('0')
			if i < len(a.digits) {
				da = a.digits[i]
			}
			if i < len(b.digits) {
				db = b.digits[i]
			}
			if da < db {
				cmp = -1
				break
			}
			if da > db {
				cmp = 1
				break
			}
		}
	}
	if a.negative {
		return -cmp
	}
	return cmp
}
