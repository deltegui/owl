package owl

import (
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/deltegui/valtruc"

	"github.com/deltegui/owl/v3/core"
	"github.com/deltegui/owl/v3/island/client"
	"github.com/deltegui/owl/v3/localizer"
)

// JsonInvalid answers 422 with what ctx.Validate found, by field and translated
// with texts (the names of the fields come from there). It is the answer an
// island reads with client.FieldErrors.
//
// The fields are named like the JSON of the model: the Go name with the first
// letter of each part in lower case ("Lines[0].Account" is "lines[0].account"),
// so a model whose JSON tags say something else needs its own answer.
//
// A missing value is the only thing worth saying about a field: the other rules
// would only repeat it.
func (ctx Ctx) JsonInvalid(texts localizer.Source) error {
	loc := ctx.Texts(texts)
	errs := map[string][]string{}
	for field, list := range ctx.ModelState.Errors {
		name := jsonPath(field)
		for _, err := range list {
			if err.GetIdentifier() == valtruc.RequiredIdentifier {
				errs[name] = []string{FormatError(loc, err)}
				break
			}
			errs[name] = append(errs[name], FormatError(loc, err))
		}
	}
	ctx.Status(http.StatusUnprocessableEntity)
	return ctx.Json(client.Errors{Errors: errs})
}

// JsonFailure answers 400 with the message of a rule of the domain, translated
// with the errors of AddLocalization (by the code of the error). It is the
// answer an island reads with client.ErrorMessage.
func (ctx Ctx) JsonFailure(err core.DomainError) error {
	ctx.Status(http.StatusBadRequest)
	return ctx.Json(client.Errors{Message: ctx.LocalizeError(err)})
}

func jsonPath(path string) string {
	parts := strings.Split(path, ".")
	for i, part := range parts {
		r, size := utf8.DecodeRuneInString(part)
		parts[i] = string(unicode.ToLower(r)) + part[size:]
	}
	return strings.Join(parts, ".")
}
