package internal

// UnusedType is an exported type never referenced outside this package.
type UnusedType struct {
	Name  string
	Value int
}

// UnusedConst is an exported constant never referenced outside this package.
const UnusedConst = 42

// UsedInternally is used within this package only (not exported outside).
func UsedInternally() string {
	t := UnusedType{Name: "test", Value: UnusedConst}
	return t.Name
}
