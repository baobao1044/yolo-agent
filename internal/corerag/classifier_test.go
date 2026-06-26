package corerag

import "testing"

func TestClassifyGo(t *testing.T) {
	cases := []struct {
		name string
		sym  Symbol
		want NodeType
	}{
		{"http handler -> Controller", Symbol{
			Name: "Handler.ServeHTTP", Kind: "method",
			Signature: "func (Handler) ServeHTTP(w http.ResponseWriter, r *http.Request)",
		}, NodeController},
		{"struct with json tags -> DTO", Symbol{
			Name: "User", Kind: "type", Signature: "type User struct",
			Body: `struct {
	ID   int    ` + "`json:\"id\"`" + `
	Name string ` + "`json:\"name\"`" + `
}`,
		}, NodeDTO},
		{"Service suffix -> Service", Symbol{
			Name: "UserService", Kind: "function", Signature: "func NewUserService() *UserService",
		}, NodeService},
		{"logger util -> Utility", Symbol{
			Name: "logger", Kind: "function", Signature: "func logInfo(msg string)",
		}, NodeUtility},
		{"plain func -> Other", Symbol{
			Name: "compute", Kind: "function", Signature: "func compute(x int) int",
		}, NodeOther},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Classify(c.sym, "go"); got != c.want {
				t.Fatalf("Classify(go) = %q, want %q", got, c.want)
			}
		})
	}
}

func TestClassifyPython(t *testing.T) {
	cases := []struct {
		name string
		sym  Symbol
		want NodeType
	}{
		{"route decorator -> Controller", Symbol{
			Name: "get_user", Signature: "def get_user(id):",
			Doc:  "@app.route('/users/<id>')",
		}, NodeController},
		{"BaseModel -> DTO", Symbol{
			Name: "User", Signature: "class User(BaseModel):",
			Body: "name: str\nage: int",
		}, NodeDTO},
		{"*Service -> Service", Symbol{
			Name: "OrderService", Signature: "class OrderService:",
		}, NodeService},
		{"logger util -> Utility", Symbol{
			Name: "logger", Signature: "def logger():",
		}, NodeUtility},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Classify(c.sym, "python"); got != c.want {
				t.Fatalf("Classify(python) = %q, want %q", got, c.want)
			}
		})
	}
}

func TestClassifyJava(t *testing.T) {
	cases := []struct {
		name string
		sym  Symbol
		want NodeType
	}{
		{"@RestController -> Controller", Symbol{
			Name: "UserController", Signature: "class UserController",
			Doc:  "@RestController",
		}, NodeController},
		{"@Entity -> DTO", Symbol{
			Name: "User", Signature: "class User",
			Doc:  "@Entity",
		}, NodeDTO},
		{"@Service -> Service", Symbol{
			Name: "UserService", Signature: "class UserService",
			Doc:  "@Service",
		}, NodeService},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Classify(c.sym, "java"); got != c.want {
				t.Fatalf("Classify(java) = %q, want %q", got, c.want)
			}
		})
	}
}

func TestClassifyUnknownLang(t *testing.T) {
	if got := Classify(Symbol{Name: "x"}, "rust"); got != NodeOther {
		t.Fatalf("Classify(unknown) = %q, want Other", got)
	}
}
