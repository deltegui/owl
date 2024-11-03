package templ

import (
	"html/template"
	"strings"

	"github.com/deltegui/owl"
	"github.com/deltegui/owl/localizer"
)

func placeErrorList(vm owl.ViewModel, key string) template.HTML {
	output := strings.Builder{}

	errs := vm.GetAllFormErrors(key)
	if len(errs) == 0 {
		return template.HTML("")
	}

	output.WriteString("<span><ul>")
	for _, err := range errs {
		output.WriteString(`<li class="form-error">` + err + `</li>`)
	}
	output.WriteString("</ul></span>")
	return template.HTML(output.String())
}

func upperCase(v string) string {
	return strings.ToUpper(v)
}

func stringNotEmpty(v string) bool {
	return len(v) > 0
}

func boolToYesNo(b bool) string {
	if b {
		//return loc.Get("shared.yes")
		return "Sí"
	}
	//return loc.Get("shared.no")
	return "No"
}

func createSelectListViewModel(loc localizer.Localizer, name string, items []owl.SelectItem, multiple bool) owl.ViewModel {
	list := owl.SelectList{
		Name:     name,
		Multiple: multiple,
		Items:    items,
	}
	return owl.ViewModel{
		Model:     list,
		Localizer: loc,
	}
}

func createYesNoSelectListViewModel(loc localizer.Localizer, name string, value *bool) owl.ViewModel {
	items := []owl.SelectItem{
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
	list := owl.SelectList{
		Name:     name,
		Multiple: false,
		Items:    items,
	}
	return owl.ViewModel{
		Model:     list,
		Localizer: loc,
	}
}

func selectList(loc localizer.Localizer, list owl.SelectList) owl.ViewModel {
	return owl.ViewModel{
		Localizer: loc,
		Model:     list,
	}
}

func CreateDefaultFuncMap() template.FuncMap {
	return template.FuncMap{
		"Uppercase":      upperCase,
		"StringNotEmpty": stringNotEmpty,
		"BoolToYesNo":    boolToYesNo,
		"SelectList":     selectList,
		"CreateSelectList": func(loc localizer.Localizer, name string, items []owl.SelectItem) owl.ViewModel {
			return createSelectListViewModel(loc, name, items, false)
		},
		"CreateMultipleSelectList": func(loc localizer.Localizer, name string, items []owl.SelectItem) owl.ViewModel {
			return createSelectListViewModel(loc, name, items, true)
		},
		"YesNoSelectList": createYesNoSelectListViewModel,
		"PlaceErrorList":  placeErrorList,
	}
}
