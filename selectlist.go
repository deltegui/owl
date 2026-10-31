package owl

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
