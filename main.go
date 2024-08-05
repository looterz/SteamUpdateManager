package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type Game struct {
	Name               string
	AutoUpdateBehavior string
}

type SteamLibrary struct {
	Path  string
	Games []Game
}

func main() {
	a := app.New()
	w := a.NewWindow("Steam Update Manager")

	libraries := detectSteamLibraries()
	var selectedLibrary *SteamLibrary

	// Log box
	logBox := widget.NewMultiLineEntry()
	logBox.Disable()

	// Games list
	gamesList := widget.NewList(
		func() int {
			if selectedLibrary == nil {
				return 0
			}
			return len(selectedLibrary.Games)
		},
		func() fyne.CanvasObject {
			return container.NewHBox(widget.NewLabel("Game"), widget.NewLabel("Auto-update"))
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			if selectedLibrary == nil {
				return
			}
			game := selectedLibrary.Games[id]
			item.(*fyne.Container).Objects[0].(*widget.Label).SetText(game.Name)
			item.(*fyne.Container).Objects[1].(*widget.Label).SetText(game.AutoUpdateBehavior)
		},
	)

	// Library selection
	librarySelect := widget.NewSelect(
		getLibraryPaths(libraries),
		func(value string) {
			for i := range libraries {
				if libraries[i].Path == value {
					selectedLibrary = &libraries[i]
					gamesList.Refresh()
					break
				}
			}
		},
	)

	// Update behavior selection
	updateBehavior := widget.NewSelect(
		[]string{"0 - Always keep this game updated", "1 - Only update this game when I launch it"},
		nil,
	)
	updateBehavior.SetSelected("1 - Only update this game when I launch it")

	// Update button
	updateButton := widget.NewButton("Update Selected Library", func() {
		if selectedLibrary == nil {
			return
		}
		go func() {
			updatedGames := updateLibrary(selectedLibrary, updateBehavior.Selected[:1], logBox)
			selectedLibrary.Games = updatedGames
			for i, lib := range libraries {
				if lib.Path == selectedLibrary.Path {
					libraries[i] = *selectedLibrary
					break
				}
			}
			gamesList.Refresh()
		}()
	})

	// Layout
	content := container.NewVBox(
		widget.NewLabel("Select Steam Library:"),
		librarySelect,
		widget.NewLabel("Games in Selected Library:"),
		container.NewVScroll(gamesList),
		widget.NewLabel("Select New Auto-Update Behavior:"),
		updateBehavior,
		updateButton,
		widget.NewLabel("Operation Log:"),
		container.NewVScroll(logBox),
	)

	// Set minimum sizes for scrollable areas
	gamesListScroll := content.Objects[3].(*container.Scroll)
	gamesListScroll.SetMinSize(fyne.NewSize(500, 200))

	logBoxScroll := content.Objects[8].(*container.Scroll)
	logBoxScroll.SetMinSize(fyne.NewSize(500, 100))

	w.SetContent(content)
	w.Resize(fyne.NewSize(600, 600))
	w.ShowAndRun()
}

func detectSteamLibraries() []SteamLibrary {
	libraries := []SteamLibrary{}

	// Detect default Steam installation path
	defaultPaths := []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Steam"),
		filepath.Join(os.Getenv("ProgramFiles"), "Steam"),
		"C:\\Steam",
	}

	for _, path := range defaultPaths {
		if _, err := os.Stat(path); err == nil {
			libraries = append(libraries, SteamLibrary{Path: path, Games: detectGames(path)})
			break
		}
	}

	// Detect additional libraries from libraryfolders.vdf
	if len(libraries) > 0 {
		vdfPath := filepath.Join(libraries[0].Path, "steamapps", "libraryfolders.vdf")
		additionalLibraries := parseLibraryFoldersVDF(vdfPath)
		for _, libPath := range additionalLibraries {
			if libPath != libraries[0].Path {
				libraries = append(libraries, SteamLibrary{Path: libPath, Games: detectGames(libPath)})
			}
		}
	}

	return libraries
}

func parseLibraryFoldersVDF(path string) []string {
	libraries := []string{}
	file, err := os.Open(path)
	if err != nil {
		return libraries
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	pathRegex := regexp.MustCompile(`^\s*"path"\s*"(.+)"`)

	for scanner.Scan() {
		line := scanner.Text()
		if match := pathRegex.FindStringSubmatch(line); match != nil {
			libraries = append(libraries, strings.ReplaceAll(match[1], "\\\\", "\\"))
		}
	}

	return libraries
}

func detectGames(libraryPath string) []Game {
	var games []Game
	steamappsPath := filepath.Join(libraryPath, "steamapps")
	files, err := ioutil.ReadDir(steamappsPath)
	if err != nil {
		fmt.Println("Error reading steamapps directory:", err)
		return games
	}

	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".acf") {
			game := parseACF(filepath.Join(steamappsPath, file.Name()))
			if game != nil {
				games = append(games, *game)
				fmt.Printf("Detected game: %s (AutoUpdateBehavior: %s)\n", game.Name, game.AutoUpdateBehavior)
			}
		}
	}

	fmt.Printf("Detected %d games in %s\n", len(games), libraryPath)
	return games
}

func parseACF(filePath string) *Game {
	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		fmt.Println("Error reading ACF file:", err)
		return nil
	}

	// Simple parser for ACF format
	lines := strings.Split(string(content), "\n")
	game := &Game{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		parts := strings.SplitN(line, "\t\t", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.Trim(parts[0], "\"")
		value := strings.Trim(parts[1], "\"")

		switch key {
		case "name":
			game.Name = value
		case "AutoUpdateBehavior":
			game.AutoUpdateBehavior = value
		}
	}

	if game.Name == "" {
		fmt.Println("Game name not found in ACF file:", filePath)
		return nil
	}
	if game.AutoUpdateBehavior == "" {
		game.AutoUpdateBehavior = "0" // Default behavior
	}

	return game
}

func updateLibrary(lib *SteamLibrary, newBehavior string, logBox *widget.Entry) []Game {
	steamappsPath := filepath.Join(lib.Path, "steamapps")
	var wg sync.WaitGroup
	updatedGames := make([]Game, len(lib.Games))
	copy(updatedGames, lib.Games)

	for i, game := range updatedGames {
		wg.Add(1)
		go func(i int, game Game) {
			defer wg.Done()
			// Find the correct appmanifest file
			files, err := filepath.Glob(filepath.Join(steamappsPath, "appmanifest_*.acf"))
			if err != nil {
				logBox.SetText(logBox.Text + fmt.Sprintf("Error searching for %s: %v\n", game.Name, err))
				return
			}
			for _, file := range files {
				content, err := ioutil.ReadFile(file)
				if err != nil {
					continue
				}
				if strings.Contains(string(content), fmt.Sprintf(`"name"		"%s"`, game.Name)) {
					err := updateACF(file, newBehavior)
					if err != nil {
						logBox.SetText(logBox.Text + fmt.Sprintf("Failed to update %s: %v\n", game.Name, err))
					} else {
						logBox.SetText(logBox.Text + fmt.Sprintf("Successfully updated %s\n", game.Name))
						updatedGames[i].AutoUpdateBehavior = newBehavior
					}
					return
				}
			}
			logBox.SetText(logBox.Text + fmt.Sprintf("Failed to find appmanifest for %s\n", game.Name))
		}(i, game)
	}
	wg.Wait()
	logBox.SetText(logBox.Text + "Update complete.\n")

	return updatedGames
}

func updateACF(filePath, newBehavior string) error {
	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	updated := false
	newLines := make([]string, 0, len(lines)+1)

	for _, line := range lines {
		if strings.Contains(line, `"AutoUpdateBehavior"`) {
			newLines = append(newLines, fmt.Sprintf(`	"AutoUpdateBehavior"		"%s"`, newBehavior))
			updated = true
		} else {
			newLines = append(newLines, line)
		}

		// Insert the new setting just before the closing brace if it doesn't exist
		if !updated && strings.TrimSpace(line) == "}" {
			newLines = append(newLines[:len(newLines)-1],
				fmt.Sprintf(`	"AutoUpdateBehavior"		"%s"`, newBehavior),
				"}")
			updated = true
		}
	}

	// If the setting wasn't found and added, add it at the end (just in case)
	if !updated {
		newLines = append(newLines[:len(newLines)-1],
			fmt.Sprintf(`	"AutoUpdateBehavior"		"%s"`, newBehavior),
			"}")
	}

	return ioutil.WriteFile(filePath, []byte(strings.Join(newLines, "\n")), 0644)
}

func getLibraryPaths(libraries []SteamLibrary) []string {
	paths := make([]string, len(libraries))
	for i, lib := range libraries {
		paths[i] = lib.Path
	}
	return paths
}
