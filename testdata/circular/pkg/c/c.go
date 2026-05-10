package c

import "example.com/circular/pkg/a"

func C() string { return "c" + a.A() }
