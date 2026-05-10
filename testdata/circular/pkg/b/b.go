package b

import "example.com/circular/pkg/c"

func B() string { return "b" + c.C() }
