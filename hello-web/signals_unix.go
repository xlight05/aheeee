//go:build unix

package main

import "syscall"

var syscallTerm = syscall.SIGTERM
