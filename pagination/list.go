package pagination

type List[T any] struct {
	Pagination Pagination
	Items      []T
}

type Order int

const (
	OrderDescending Order = 0
	OrderAscending  Order = 1
)

type Pagination struct {
	CurrentPage     int
	ElementsPerPage int
	TotalElements   int
	Order           Order
	OrderBy         string
	Enabeld         bool
}
