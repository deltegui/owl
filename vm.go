package owl

import (
	"html/template"

	"github.com/deltegui/owl/core"
	"github.com/deltegui/owl/csrf"
	"github.com/deltegui/owl/localizer"
)

type ViewModel struct {
	Model      any
	Localizer  localizer.Localizer
	ModelState core.ModelState
	CsrfToken  string
	Ctx        Ctx
}

func createViewModel(ctx Ctx, name string, model any) ViewModel {
	var loc = ctx.GetLocalizer(name)
	csrfToken, ok := ctx.Get(csrf.ContextKey).(string)
	if !ok {
		csrfToken = ""
	}
	return ViewModel{
		Model:      model,
		CsrfToken:  csrfToken,
		Localizer:  loc,
		ModelState: ctx.ModelState,
		Ctx:        ctx,
	}
}

func (vm ViewModel) PlaceCsrfInput() template.HTML {
	return template.HTML(`<input type="hidden" name="` + csrf.CsrfHeaderName + `" value="` + vm.CsrfToken + `"/>`)
}

func (vm ViewModel) Localize(key string, args ...any) string {
	return vm.Localizer.GetFormatted(key, args...)
}

func (vm ViewModel) LocalizeError(err core.DomainError) string {
	return vm.Ctx.LocalizeError(err)
}

func (vm ViewModel) HaveFormError(key string) bool {
	if vm.ModelState.Valid {
		return false
	}
	_, ok := vm.ModelState.Errors[key]
	return ok
}

func (vm ViewModel) GetFormError(key string) string {
	if vm.ModelState.Valid {
		return ""
	}
	val, ok := vm.ModelState.Errors[key]
	if !ok {
		return ""
	}
	if len(val) == 0 {
		return ""
	}
	return vm.formatError(val[0])
}

func (vm ViewModel) formatError(err core.ValidationError) string {
	locKey := vm.Localize(err.GetFieldName())
	locVal := vm.Localize(string(err.GetIdentifier()), locKey)
	finalVal := err.Format(locVal)
	return finalVal
}

func (vm ViewModel) GetAllFormErrors(key string) []string {
	output := []string{}
	if vm.ModelState.Valid {
		return output
	}
	val, ok := vm.ModelState.Errors[key]
	if !ok {
		return output
	}
	for _, err := range val {
		output = append(output, vm.formatError(err))
	}
	return output
}

type SelectItem struct {
	Value    string
	Tag      string
	Selected bool
}

type SelectList struct {
	Name     string
	Multiple bool
	Filtered bool
	Items    []SelectItem
}

func (list *SelectList) ClearSelection() {
	for i := 0; i < len(list.Items); i++ {
		list.Items[i].Selected = false
	}
}

func (list *SelectList) Select(v string) {
	list.ClearSelection()
	for i := 0; i < len(list.Items); i++ {
		if list.Items[i].Value == v {
			list.Items[i].Selected = true
		}
	}
}

func CreateSelectListViewModel(loc localizer.Localizer, name string, items []SelectItem, multiple bool) ViewModel {
	list := SelectList{
		Name:     name,
		Multiple: multiple,
		Items:    items,
	}
	return ViewModel{
		Model:     list,
		Localizer: loc,
	}
}

func CreateYesNoSelectListViewModel(loc localizer.Localizer, name string, value *bool) ViewModel {
	items := []SelectItem{
		{
			Value:    "",
			Tag:      "shared.choose",
			Selected: value == nil,
		},
		{
			Value:    "1",
			Tag:      "shared.yes",
			Selected: value != nil && *value,
		},
		{
			Value:    "0",
			Tag:      "shared.no",
			Selected: value != nil && !*value,
		},
	}
	list := SelectList{
		Name:     name,
		Multiple: false,
		Items:    items,
	}
	return ViewModel{
		Model:     list,
		Localizer: loc,
	}
}
