package templ

import (
	"html/template"
	"log"
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

func selectList(loc localizer.Localizer, list owl.SelectList) owl.ViewModel {
	return owl.ViewModel{
		Localizer: loc,
		Model:     list,
	}
}

func boolToYesNo(b bool) string {
	if b {
		//return loc.Get("shared.yes")
		return "Sí"
	}
	//return loc.Get("shared.no")
	return "No"
}

func variadicToArray(elems ...any) []any {
	return elems
}

func paramsMap(elems ...any) map[string]any {
	if len(elems)%2 != 0 {
		log.Println("Invalid length of argument list passed to map function in view.")
		return map[string]any{}
	}
	var places int = len(elems) / 2

	output := make(map[string]any, places)

	for i := 0; i < len(elems); i += 2 {
		name := elems[i].(string)
		value := elems[i+1]
		output[name] = value
	}

	return output
}

func mapKeyExists(m map[string]any, key string) bool {
	_, ok := m[key]
	return ok
}

func CreateDefaultFuncMap() template.FuncMap {
	return template.FuncMap{
		"Uppercase":      upperCase,
		"StringNotEmpty": stringNotEmpty,
		"SelectList":     selectList,
		"BoolToYesNo":    boolToYesNo,
		"CreateSelectList": func(loc localizer.Localizer, name string, items []owl.SelectItem) owl.ViewModel {
			return createSelectListViewModel(loc, name, items, false)
		},
		"CreateMultipleSelectList": func(loc localizer.Localizer, name string, items []owl.SelectItem) owl.ViewModel {
			return createSelectListViewModel(loc, name, items, true)
		},
		"PlaceErrorList": placeErrorList,
		"Arr":            variadicToArray,
		"Map":            paramsMap,
		"MapKeyExists":   mapKeyExists,
	}
}
