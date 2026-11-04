package main

import (
	"os"

	"example.com/helper"
)

func main() {
	_, _ = os.Open("x")
	_, _ = helper.LoadFile("x")
}
