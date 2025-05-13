package csrf

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/deltegui/owl/core"
	"github.com/deltegui/owl/cypher"
)

// CsrfHeaderName defines the name of a
// CSRF token in a HTTP request.
const CsrfHeaderName string = "X-Csrf-Token"

// Csrf gives an API to create and check Csrf tokens.
type Csrf struct {
	cipher  core.Cypher
	expires time.Duration
}

// Creates new Csrf token. Requires the expiration time and
// a Cypher implementation to encrypt and decrypt the tokens.
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

const tokenDelimiter string = "::"

// Generates a random csrf token
func (csrf Csrf) Generate() string {
	unixTime := time.Now().Unix()
	random := core.GenerateToken(core.Size64)
	raw := fmt.Sprintf("%s%s%d", random, tokenDelimiter, unixTime)
	e := csrf.encrypt(raw)
	return e
}

// Checks a csrf token. Returns an nil error if the csrf token is valid. Otherwise
// will return an error telling why the token is invalid.
func (csrf Csrf) Check(token string) error {
	raw, err := csrf.decrypt(token)
	if err != nil {
		return fmt.Errorf("cannot decrypt csrf token: %w", err)
	}
	parts := strings.Split(raw, tokenDelimiter)
	const minimumParts = 2
	if len(parts) < minimumParts {
		return fmt.Errorf("malformed csrf token: not enough parts")
	}
	unixTime := parts[1]
	i, err := strconv.ParseInt(unixTime, core.IntBase10, core.Size64)
	if err != nil {
		return fmt.Errorf("malformed csrf token: unixtime is not int64")
	}
	t := time.Unix(i, 0)
	expirationTime := t.Add(csrf.expires)
	if expirationTime.Before(time.Now()) {
		return fmt.Errorf("expired csrf token")
	}
	return nil
}

// CheckRequest checks a Csrf token sent in an HTTP Request. Returns an nil error
// if the token is valid or ignored. Returns an error if the token is not valid.
// The token is ignored if the http method is GET or HEAD.
// The token is searched using the header with a name defined in the constant CsrfHeaderName.
func (csrf Csrf) CheckRequest(req *http.Request) error {
	if req.Method == http.MethodGet || req.Method == http.MethodHead {
		return nil
	}

	token := req.FormValue(CsrfHeaderName)
	if len(token) == 0 {
		token = req.Header.Get(CsrfHeaderName)
		if len(token) == 0 {
			return fmt.Errorf("csrf header (%s) token not found", CsrfHeaderName)
		}
		return csrf.Check(token)
	}
	return csrf.Check(token)
}
