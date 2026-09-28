package ids

import (
	"crypto/rand"
	"encoding/hex"
	"math/big"
)

const slugAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"

func NewSlug() string {
	b := make([]byte, 10)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(slugAlphabet))))
		if err != nil {
			panic(err)
		}
		b[i] = slugAlphabet[n.Int64()]
	}
	return string(b)
}

func NewToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
