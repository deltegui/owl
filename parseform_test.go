package owl

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func formRequest(values url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func parse(req *http.Request, dst any) { Ctx{Req: req}.ParseForm(dst) }

func TestParseFormReadsEveryKindOfField(t *testing.T) {
	type form struct {
		S   string
		B   bool
		I   int
		I8  int8
		I16 int16
		I32 int32
		I64 int64
		U   uint
		U8  uint8
		U16 uint16
		U32 uint32
		U64 uint64
		F32 float32
		F64 float64
		T   time.Time
	}
	var got form
	parse(formRequest(url.Values{
		"S": {"hola"}, "B": {"true"}, "I": {"-5"}, "I8": {"-128"}, "I16": {"-32768"},
		"I32": {"-2147483648"}, "I64": {"-9223372036854775808"}, "U": {"7"}, "U8": {"255"},
		"U16": {"65535"}, "U32": {"4294967295"}, "U64": {"18446744073709551615"},
		"F32": {"1.5"}, "F64": {"2.25"}, "T": {"2026-11-02T10:30:00Z"},
	}), &got)
	want := form{
		S: "hola", B: true, I: -5, I8: -128, I16: -32768, I32: -2147483648, I64: -9223372036854775808,
		U: 7, U8: 255, U16: 65535, U32: 4294967295, U64: 18446744073709551615,
		F32: 1.5, F64: 2.25, T: time.Date(2026, 11, 2, 10, 30, 0, 0, time.UTC),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestParseFormPointersAreFilledOnlyWithAValue(t *testing.T) {
	type form struct {
		S *string
		B *bool
		I *int
		U *uint
		F *float64
		T *time.Time
	}
	var got form
	parse(formRequest(url.Values{
		"S": {"x"}, "B": {"false"}, "I": {"3"}, "U": {"4"}, "F": {"0.5"}, "T": {"2026-01-02T03:04:05Z"},
	}), &got)
	if got.S == nil || *got.S != "x" || got.B == nil || *got.B || got.I == nil || *got.I != 3 ||
		got.U == nil || *got.U != 4 || got.F == nil || *got.F != 0.5 ||
		got.T == nil || !got.T.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("pointers not filled: %+v", got)
	}

	var empty form
	parse(formRequest(url.Values{"S": {""}, "B": {""}, "I": {""}, "U": {""}, "F": {""}, "T": {""}}), &empty)
	if empty != (form{}) {
		t.Fatalf("an empty value must leave the pointer nil: %+v", empty)
	}
}

func TestParseFormUsesTheHTMLTagThenTheFieldName(t *testing.T) {
	type form struct {
		Page  int    `html:"pagination.currentPage"`
		Title string // no tag: the name of the field
	}
	var got form
	parse(formRequest(url.Values{"pagination.currentPage": {"2"}, "Title": {"t"}, "Page": {"9"}}), &got)
	if got.Page != 2 || got.Title != "t" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseFormLeavesWhatTheFormDoesNotSayOrCannotBeRead(t *testing.T) {
	type form struct {
		Missing int
		Bad     int
		Over    int8
		Neg     uint
		Flag    bool
		When    time.Time
		Ratio   float32
		hidden  int
	}
	got := form{Missing: 5, Bad: 6, Over: 7, Neg: 8, Flag: true, When: time.Unix(1, 0), Ratio: 9, hidden: 10}
	before := got
	parse(formRequest(url.Values{
		"Bad": {"abc"}, "Over": {"128"}, "Neg": {"-1"}, "Flag": {"maybe"}, "When": {"ayer"},
		"Ratio": {"1e40"}, "hidden": {"1"},
	}), &got)
	if got != before {
		t.Fatalf("a value that is missing or cannot be read must not change the field:\ngot  %+v\nwant %+v", got, before)
	}
}

func TestParseFormIgnoresWhatIsNotAPointerToAStruct(t *testing.T) {
	n := 4
	parse(formRequest(url.Values{"n": {"9"}}), &n) // must not panic
	if n != 4 {
		t.Fatalf("n = %d", n)
	}
}

func TestParseFormReadsTheQueryOfAGet(t *testing.T) {
	var got struct{ Q string }
	parse(httptest.NewRequest(http.MethodGet, "/?Q=buscar", nil), &got)
	if got.Q != "buscar" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseFormReadsMultipart(t *testing.T) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("Name", "ana")
	_ = w.WriteField("Age", "31")
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())

	var got struct {
		Name string
		Age  int
	}
	parse(req, &got)
	if got.Name != "ana" || got.Age != 31 {
		t.Fatalf("got %+v", got)
	}
}

func TestParseFormDoesNotParseAFormThatIsAlreadyParsed(t *testing.T) {
	req := formRequest(url.Values{"A": {"from the body"}})
	req.Form = url.Values{"A": {"already there"}}
	var got struct{ A string }
	parse(req, &got)
	if got.A != "already there" {
		t.Fatalf("A = %q", got.A)
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("A", "from the body")
	_ = w.Close()
	mp := httptest.NewRequest(http.MethodPost, "/", &body)
	mp.Header.Set("Content-Type", w.FormDataContentType())
	mp.Form = url.Values{"A": {"already there"}}
	got.A = ""
	parse(mp, &got)
	if got.A != "already there" {
		t.Fatalf("multipart A = %q", got.A)
	}
}

// setValue tells if it could set the field, and sets it only then.
func TestSetValue(t *testing.T) {
	type tc struct {
		name  string
		field any // pointer to the field
		value string
		ok    bool
		want  any // what the field holds afterwards
	}
	var (
		i   int
		i8  int8
		i16 int16
		i32 int32
		i64 int64
		u   uint
		u8  uint8
		u16 uint16
		u32 uint32
		u64 uint64
		f32 float32
		f64 float64
		b   bool
		s   string
		d   time.Time
		in  struct{ X int }
		sl  []int
	)
	tests := []tc{
		{"int hex", &i, "0x10", true, 16},
		{"int octal", &i64, "010", true, int64(8)},
		{"int8 max", &i8, "127", true, int8(127)},
		{"int8 over", &i8, "128", false, int8(0)},
		{"int16 over", &i16, "32768", false, int16(0)},
		{"int32 over", &i32, "2147483648", false, int32(0)},
		{"int64 over", &i64, "9223372036854775808", false, int64(0)},
		{"int not a number", &i, "1.5", false, 0},
		{"int empty", &i, "", false, 0},
		{"uint max", &u64, "18446744073709551615", true, uint64(18446744073709551615)},
		{"uint8 over", &u8, "256", false, uint8(0)},
		{"uint16 over", &u16, "65536", false, uint16(0)},
		{"uint32 over", &u32, "4294967296", false, uint32(0)},
		{"uint negative", &u, "-1", false, uint(0)},
		{"float32 ok", &f32, "3.5", true, float32(3.5)},
		{"float32 over", &f32, "1e40", false, float32(0)},
		{"float64 big", &f64, "1e40", true, 1e40},
		{"float not a number", &f64, "x", false, float64(0)},
		{"bool t", &b, "t", true, true},
		{"bool 0", &b, "0", true, false},
		{"bool nonsense", &b, "yes", false, false},
		{"string empty", &s, "", true, ""},
		{"string", &s, "x", true, "x"},
		{"time", &d, "2026-11-02T10:30:00+01:00", true, time.Date(2026, 11, 2, 10, 30, 0, 0, time.FixedZone("", 3600))},
		{"time empty", &d, "", false, time.Time{}},
		{"time not rfc3339", &d, "2026-11-02", false, time.Time{}},
		{"other struct", &in, "x", false, struct{ X int }{}},
		{"slice", &sl, "1", false, []int(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field := reflect.ValueOf(tt.field).Elem()
			field.Set(reflect.Zero(field.Type()))
			if ok := setValue(field, tt.value); ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			got := field.Interface()
			if gt, ok := got.(time.Time); ok {
				if !gt.Equal(tt.want.(time.Time)) {
					t.Fatalf("got %v, want %v", gt, tt.want)
				}
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

// A field the form does not mention is skipped, and the ones after it still count.
func TestParseFormSkipsAbsentFieldsAndKeepsGoing(t *testing.T) {
	type form struct {
		Absent  string // an empty string would overwrite it if it were read
		hidden  string // cannot be set
		Present string
	}
	got := form{Absent: "keep", hidden: "keep"}
	parse(formRequest(url.Values{"Present": {"yes"}}), &got)
	if got.Absent != "keep" || got.Present != "yes" {
		t.Fatalf("got %+v", got)
	}

	got = form{Absent: "keep", hidden: "keep"}
	parse(formRequest(url.Values{"hidden": {"x"}, "Present": {"yes"}}), &got)
	if got.hidden != "keep" || got.Present != "yes" {
		t.Fatalf("an unexported field is skipped and the next one is read: %+v", got)
	}
}
