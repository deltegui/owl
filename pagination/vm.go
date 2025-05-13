package pagination

import (
	"fmt"
	"math"

	"github.com/deltegui/owl"
	"github.com/deltegui/owl/localizer"
)

type ViewModel struct {
	StartElement       int
	LastElement        int
	TotalElements      int
	Sequence           []int
	PreviousPage       int
	CurrentPage        int `html:"pagination.currentPage" json:"currentPage"`
	NextPage           int
	Show               bool
	ShowPreviousButton bool
	ShowNextButton     bool
	loc                localizer.Localizer
}

func (vm ViewModel) GetMessageOfElements() string {
	return fmt.Sprintf(
		vm.loc.Get("PaginationMessageOfElements"),
		vm.StartElement,
		vm.LastElement,
		vm.TotalElements)
}

func (vm ViewModel) GetNextTag() string {
	return vm.loc.Get("PaginationNextButton")
}

func (vm ViewModel) GetPreviousTag() string {
	return vm.loc.Get("PaginationPreviousButton")
}

func (vm ViewModel) ToDto() Pagination {
	return Pagination{
		CurrentPage: vm.CurrentPage,
		Enabeld:     true,
	}
}

func ToVM(ctx owl.Ctx, p Pagination) ViewModel {
	const margin int = 4
	startElement := ((p.CurrentPage - 1) * p.ElementsPerPage)
	lastPage := int(math.Ceil(float64(p.TotalElements) / float64(p.ElementsPerPage)))
	minPag := p.CurrentPage - margin
	maxPag := p.CurrentPage + margin
	if minPag < 1 {
		minPag = 1
	}
	if maxPag > lastPage {
		maxPag = lastPage
	}
	var sequence []int
	for i := minPag; i <= maxPag; i++ {
		sequence = append(sequence, i)
	}
	return ViewModel{
		StartElement:       startElement,
		LastElement:        startElement + p.ElementsPerPage,
		TotalElements:      p.TotalElements,
		Show:               p.TotalElements > p.ElementsPerPage,
		ShowPreviousButton: p.CurrentPage > 1,
		ShowNextButton:     p.CurrentPage < lastPage,
		Sequence:           sequence,
		PreviousPage:       p.CurrentPage - 1,
		CurrentPage:        p.CurrentPage,
		NextPage:           p.CurrentPage + 1,
		loc:                ctx.GetLocalizer("common/pagination"),
	}
}
