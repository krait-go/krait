package a

import "example.com/circular/pkg/b"

func A() string { return "a" + b.B() }
