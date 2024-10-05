package owl

import (
	"fmt"
	"log"

	"github.com/deltegui/owl/core"
	"github.com/deltegui/owl/localizer"
)

type ViewModel struct {
	Model      interface{}
	Localizer  localizer.Localizer
	FormErrors map[string][]core.ValidationError
	CsrfToken  string
	Ctx        Ctx
}

func createViewModel(ctx Ctx, name string, model any) ViewModel {
	var loc = ctx.GetLocalizer(name)
	csrfToken, ok := ctx.Get("phx-csrf").(string)
	if !ok {
		csrfToken = ""
	}
	return ViewModel{
		Model:     model,
		CsrfToken: csrfToken,
		Localizer: loc,
		Ctx:       ctx,
	}
}

func (vm ViewModel) Localize(key string) string {
	return vm.Localizer.Get(key)
}

func (vm ViewModel) HaveFormError(key string) bool {
	if vm.FormErrors == nil {
		return false
	}
	_, ok := vm.FormErrors[key]
	return ok
}

func (vm ViewModel) GetFormError(key string) string {
	if vm.FormErrors == nil {
		return ""
	}
	val, ok := vm.FormErrors[key]
	if !ok {
		return ""
	}
	if len(val) == 0 {
		return ""
	}
	return vm.formatError(key, val[0])
}

func (vm ViewModel) formatError(key string, err core.ValidationError) string {
	log.Println("Error:", err)
	locVal := vm.Localize(err.GetName())
	log.Println("locVal format error", locVal)
	finalVal := err.Format(locVal)
	log.Println(finalVal)
	locKey := vm.Localize(key)
	return fmt.Sprintf(finalVal, locKey)
}

func (vm ViewModel) GetAllFormErrors(key string) []string {
	output := []string{}
	if vm.FormErrors == nil {
		return output
	}
	val, ok := vm.FormErrors[key]
	if !ok {
		return output
	}
	for _, err := range val {
		output = append(output, vm.formatError(key, err))
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