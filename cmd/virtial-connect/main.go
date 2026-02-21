package main

import (
	"fyne.io/fyne/v2/app"

	vcapp "virtial-connect/internal/app"
)

func main() {
	a := app.NewWithID("com.virtial.connect")
	ui := vcapp.New(a)
	ui.Show()
}
