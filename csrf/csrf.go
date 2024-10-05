package csrf

import (
	"crypto/rand"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/deltegui/owl/core"
	"github.com/deltegui/owl/cypher"
)

const CsrfHeaderName string = "X-Csrf-Token"

type Csrf struct {
	cipher  core.Cypher
	expires time.Duration
}

func New(expires time.Duration, cipher core.Cypher) *Csrf {
	return &Csrf{
		cipher:  cipher,
		expires: expires,
	}
}

func (csrf *Csrf) encrypt(raw string) string {
	encoded, err := cypher.EncodeCookie(csrf.cipher, raw)
	if err != nil {
		log.Println("Cannot encode csrf token:", err)
		return ""
	}
	return encoded
}

func (csrf *Csrf) decrypt(token string) (string, error) {
	decoded, err := cypher.DecodeCookie(csrf.cipher, token)
	if err != nil {
		return "", fmt.Errorf("cannot decode csrf token: %w", err)
	}
	return decoded, nil
}

func (csrf Csrf) Generate() string {
	unixTime := time.Now().Unix()
	prime, err := rand.Prime(rand.Reader, core.Size64)
	if err != nil {
		log.Panicln(err)
	}
	raw := fmt.Sprintf("%d//00//%d", prime.Int64(), unixTime)
	e := csrf.encrypt(raw)
	return e
}

func (csrf Csrf) Check(token string) bool {
	raw, err := csrf.decrypt(token)
	if err != nil {
		log.Println("Cannot decrypt csrf token: ", err)
		return false
	}
	parts := strings.Split(raw, "//00//")
	const minimumParts = 2
	if len(parts) < minimumParts {
		log.Println("Malformed csrf token. Not enough parts.")
		return false
	}
	unixTime := parts[0]
	i, err := strconv.ParseInt(unixTime, core.IntBase10, core.Size64)
	if err != nil {
		log.Println("Malformed csrf token. Unixtime is not int64.")
		return false
	}
	t := time.Unix(i, 0)
	if t.After(time.Now().Add(-csrf.expires)) {
		log.Println("Expired csrf token!")
		return false
	}
	return true
}

func (csrf Csrf) CheckRequest(req *http.Request) bool {
	token := req.FormValue(CsrfHeaderName)
	if len(token) == 0 {
		token = req.Header.Get(CsrfHeaderName)
		if len(token) == 0 {
			log.Printf("Csrf header (%s) token not found\n", CsrfHeaderName)
			return false
		}
		return csrf.Check(token)
	}
	return csrf.Check(token)
}
