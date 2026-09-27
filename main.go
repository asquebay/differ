package main

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/sergi/go-diff/diffmatchpatch"
)

const (
	Reset = "\033[0m"
	Bold  = "\033[1m"
	Red   = "\033[31m"
	Green = "\033[32m"
	Dim   = "\033[2m"
	Cyan  = "\033[36m"
)

// Проверяем соответствие интерфейсу во время компиляции
var _ tea.Model = model{}

type model struct {
	textarea textarea.Model
	prompt   string
	value    string
	quitting bool
}

func initialModel(prompt string) model {
	ta := textarea.New()
	ta.Placeholder = "Вставьте текст сюда (Ctrl+Shift+V)..."
	ta.Focus()
	ta.ShowLineNumbers = true
	ta.SetWidth(120)
	ta.SetHeight(25)

	// Вырезаем лимит символов (0 = бесконечно), чтобы Enter и текст не блокировались
	ta.CharLimit = 0
	ta.MaxWidth = 0
	// Убираем ограничение на максимальное количество строк (0 = без лимита)
	ta.MaxHeight = 0

	return model{
		textarea: ta,
		prompt:   prompt,
	}
}

func (m model) Init() tea.Cmd {
	return textarea.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// В v2 проверка управляющих комбинаций делается по их строковому значению
		switch msg.String() {
		case "ctrl+d", "ctrl+c", "ctrl+z":
			m.value = m.textarea.Value()
			m.quitting = true
			return m, tea.Quit
		}
	}

	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

// В v2 метод View() ОБЯЗАН возвращать тип tea.View
func (m model) View() tea.View {
	if m.quitting {
		return tea.View{}
	}

	str := fmt.Sprintf(
		"%s%s%s%s\n\n%s\n\n%sНажмите Ctrl+D или Ctrl+C, чтобы подтвердить ввод и продолжить%s\n",
		Bold, Cyan, m.prompt, Reset,
		m.textarea.View(),
		Dim, Reset,
	)

	// Создаём структуру представления и активируем AltScreen (полноэкранный TUI)
	v := tea.NewView(str)
	v.AltScreen = true
	return v
}

func readText(stepPrompt string) string {
	// В v2 убрана функция tea.WithAltScreen(), режим настраивается внутри View()
	p := tea.NewProgram(initialModel(stepPrompt))
	finalModel, err := p.Run()
	if err != nil {
		fmt.Printf("Ошибка TUI: %v\n", err)
		os.Exit(1)
	}

	m, _ := finalModel.(model)
	return m.value
}

// Вспомогательная структура для плоского представления строк диффа
type diffLine struct {
	Type diffmatchpatch.Operation
	Text string
	Num1 int // номер строки в файле 1 (0 если добавлена)
	Num2 int // номер строки в файле 2 (0 если удалена)
}

func main() {
	// 1. Пошаговый ввод первого и второго текстов
	prompt1 := "[1/2] Вставьте ПЕРВЫЙ текст:"
	text1 := readText(prompt1)
	fmt.Printf("%s%s[1/2] Первый текст принят%s\n", Bold, Cyan, Reset)

	prompt2 := "[2/2] Вставьте ВТОРОЙ текст:"
	text2 := readText(prompt2)
	fmt.Printf("%s%s[2/2] Второй текст принят%s\n", Bold, Cyan, Reset)

	// 2. Блок результатов
	fmt.Println(strings.Repeat("-", 40))
	fmt.Printf("%s%sРезультат сравнения%s\n", Bold, Cyan, Reset)
	fmt.Println(strings.Repeat("-", 40) + "\n")

	// 3. Расчёт диффа (построчный режим)
	dmp := diffmatchpatch.New()
	text1Runes, text2Runes, lineArray := dmp.DiffLinesToRunes(text1, text2)

	diffs := dmp.DiffMainRunes(text1Runes, text2Runes, false)
	diffs = dmp.DiffCharsToLines(diffs, lineArray)
	diffs = dmp.DiffCleanupSemantic(diffs)

	if len(diffs) == 1 && diffs[0].Type == diffmatchpatch.DiffEqual {
		fmt.Printf("%s%sТексты абсолютно идентичны!%s\n", Bold, Green, Reset)
		return
	}

	// Превращаем сырые куски в плоский список строк с расчётом реальных номеров строк
	var allLines []diffLine
	line1, line2 := 1, 1

	for _, diff := range diffs {
		if diff.Text == "" {
			continue
		}
		// Дробим кусок на строки, сохраняя финальный перенос.
		// Используем strings.SplitSeq вместо strings.Split.
		// Так как это итератор, мы передаём его напрямую в for range
		for line := range strings.SplitSeq(strings.TrimSuffix(diff.Text, "\n"), "\n") {
			dl := diffLine{Type: diff.Type, Text: line}
			switch diff.Type {
			case diffmatchpatch.DiffEqual:
				dl.Num1 = line1
				dl.Num2 = line2
				line1++
				line2++
			case diffmatchpatch.DiffDelete:
				dl.Num1 = line1
				line1++
			case diffmatchpatch.DiffInsert:
				dl.Num2 = line2
				line2++
			}
			allLines = append(allLines, dl)
		}
	}

	// 4. Логика генерации хунков (Hunks) с контекстом вокруг изменений
	const contextSize = 3
	n := len(allLines)
	show := make([]bool, n)

	// 4.1. Маркировка изменённых строк и контекста вокруг них
	for i := range n {
		if allLines[i].Type != diffmatchpatch.DiffEqual {
			// Сама изменённая строка должна быть показана
			show[i] = true
			// Маркируем контекст до неё
			for j := i - 1; j >= 0 && j >= i-contextSize; j-- {
				show[j] = true
			}
			// Маркируем контекст после неё
			for j := i + 1; j < n && j <= i+contextSize; j++ {
				show[j] = true
			}
		}
	}

	// 4.2. Вывод сгруппированных хунков с метаинформацией @@
	inHunk := false
	var hunkLines []diffLine

	for i := range n {
		if show[i] {
			if !inHunk {
				inHunk = true
				hunkLines = []diffLine{}
			}
			hunkLines = append(hunkLines, allLines[i])
		}

		// Если хунк закончился или мы дошли до конца всего списка строк
		if (!show[i] || i == n-1) && inHunk {
			inHunk = false
			if len(hunkLines) == 0 {
				continue
			}

			// Считываем метаданные для заголовка @@
			start1, len1 := 0, 0
			start2, len2 := 0, 0

			for _, hl := range hunkLines {
				switch hl.Type {
				case diffmatchpatch.DiffEqual:
					if start1 == 0 {
						start1 = hl.Num1
					}
					if start2 == 0 {
						start2 = hl.Num2
					}
					len1++
					len2++

				case diffmatchpatch.DiffDelete:
					if start1 == 0 {
						start1 = hl.Num1
					}
					len1++

				case diffmatchpatch.DiffInsert:
					if start2 == 0 {
						start2 = hl.Num2
					}
					len2++
				}
			}

			// Если хунк зацепил строки с начала, где номера ещё не успели посчитаться из-за вставок
			if start1 == 0 {
				start1 = 1
			}
			if start2 == 0 {
				start2 = 1
			}

			// Печатаем заголовок хунка в стиле git (голубым цветом)
			fmt.Printf("%s@@ -%d,%d +%d,%d @@%s\n", Cyan, start1, len1, start2, len2, Reset)

			// Печатаем строки самого хунка
			for _, hl := range hunkLines {
				switch hl.Type {
				case diffmatchpatch.DiffDelete:
					// 2 пробела для выравнивания удалённых строк
					fmt.Printf("%s  %s%s\n", Red, hl.Text, Reset)
				case diffmatchpatch.DiffInsert:
					// 2 пробела для выравнивания добавленных строк
					fmt.Printf("%s  %s%s\n", Green, hl.Text, Reset)
				case diffmatchpatch.DiffEqual:
					fmt.Printf("%s  %s%s\n", Dim, hl.Text, Reset)
				}
			}
			fmt.Println() // пустая строка между блоками изменений для читаемости
		}
	}
}
