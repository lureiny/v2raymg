package main

func main() {
	if err := loadConfig(); err != nil {
		panic(err)
	}

	prompt := initPromptAndRegister()
	if err := prompt.Run(); err != nil {
		panic(err)
	}
}
