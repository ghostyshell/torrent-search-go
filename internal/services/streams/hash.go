package streams

import (
	"fmt"
	"strings"
)

const base32Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"

// Base32ToHex converts a base32 info-hash to hex.
func Base32ToHex(base32 string) (string, error) {
	var bits, value int
	var hex strings.Builder
	for _, char := range strings.ToUpper(base32) {
		idx := strings.IndexRune(base32Alphabet, char)
		if idx < 0 {
			return "", fmt.Errorf("invalid base32 char: %c", char)
		}
		value = (value << 5) | idx
		bits += 5
		if bits >= 8 {
			hex.WriteByte(byte((value >> (bits - 8)) & 0xff))
			bits -= 8
		}
	}
	return hex.String(), nil
}
