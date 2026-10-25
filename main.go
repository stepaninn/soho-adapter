package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка:", err)
		os.Exit(1)
	}
}

func run() error {
	fmt.Println("SOHO LMS: адаптер чтения")
	fmt.Println("Каркас программы запущен")

	return nil
}
