//go:build !unix

package main

import "os"

var syscallTerm os.Signal = os.Interrupt
