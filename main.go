package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Data model ────────────────────────────────────────────────────────────────

type GitInfo struct {
	Branch   string
	Dirty    bool
	Root     string
	MainRoot string // main worktree root (same as Root for non-worktrees)
	Name     string
}

type AgentState int

const (
	StateNone      AgentState = iota // no agent / inactive
	StateRunning                     // executing tool calls
	StateThinking                    // LLM inference, no tool calls
	StateDone                        // task complete, awaiting new prompt
	StateAsking                      // agent presented a question or choice
	StatePlanReady                   // plan ready, awaiting approval
)

type Pane struct {
	ID      string
	PID     string
	Command string
	Dir     string
	Title   string
	Session string
	Git   *GitInfo
	Agent AgentState
}

type RepoGroup struct {
	Name  string
	Git   *GitInfo
	Panes []Pane
}

type ItemKind int

const (
	KindRepo ItemKind = iota
	KindPane
	KindWorktree
)

type Item struct {
	Kind   ItemKind
	Label  string
	Target string
	Dir   string
	Agent AgentState
}

type appState int

const (
	stateBrowsing appState = iota
	stateConfirmKill
	statePickRepo
)

type viewMode int

const (
	viewList        viewMode = iota
	viewKanban               // repo board: one column per git repo
	viewKanbanDrill          // drill-down: Waiting / Running / Done for one repo
)

type groupMode int

const (
	groupByProject groupMode = iota
	groupByDir
	groupBySession
)

const (
	kanbanCardHeight  = 4  // 2 content lines + top/bottom border
	drillCardHeight   = 5  // 3 content lines + top/bottom border
	kanbanMinColWidth = 26 // minimum comfortable column width
)

var drillColNames = [3]string{"Waiting", "Running", "Done"}

// ── Preferences persistence ──────────────────────────────────────────────────

func prefsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "tmux-nav")
}

func loadGroupMode() groupMode {
	data, err := os.ReadFile(filepath.Join(prefsDir(), "grouping"))
	if err != nil {
		return groupByProject
	}
	switch strings.TrimSpace(string(data)) {
	case "directory":
		return groupByDir
	case "session":
		return groupBySession
	default:
		return groupByProject
	}
}

func saveGroupMode(g groupMode) {
	dir := prefsDir()
	os.MkdirAll(dir, 0o755)
	var val string
	switch g {
	case groupByDir:
		val = "directory"
	case groupBySession:
		val = "session"
	default:
		val = "project"
	}
	os.WriteFile(filepath.Join(dir, "grouping"), []byte(val+"\n"), 0o644)
}

func loadAgentOnly() bool {
	data, err := os.ReadFile(filepath.Join(prefsDir(), "agent_only"))
	if err != nil {
		return true // default to agent-only
	}
	return strings.TrimSpace(string(data)) == "true"
}

func saveAgentOnly(v bool) {
	dir := prefsDir()
	os.MkdirAll(dir, 0o755)
	val := "false"
	if v {
		val = "true"
	}
	os.WriteFile(filepath.Join(dir, "agent_only"), []byte(val+"\n"), 0o644)
}

func loadViewMode() viewMode {
	data, err := os.ReadFile(filepath.Join(prefsDir(), "view_mode"))
	if err != nil {
		return viewList
	}
	switch strings.TrimSpace(string(data)) {
	case "kanban":
		return viewKanban
	default:
		return viewList
	}
}

func saveViewMode(v viewMode) {
	dir := prefsDir()
	os.MkdirAll(dir, 0o755)
	val := "list"
	if v == viewKanban {
		val = "kanban"
	}
	os.WriteFile(filepath.Join(dir, "view_mode"), []byte(val+"\n"), 0o644)
}

// repoKey returns a stable map key for a pane's repository.
func repoKey(p Pane) string {
	if p.Git != nil {
		return p.Git.MainRoot
	}
	return "__no_repo__"
}

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	styleTitle         = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212")).Padding(0, 1)
	styleRepo          = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	styleWorktree      = lipgloss.NewStyle().Foreground(lipgloss.Color("110"))
	stylePane          = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	styleSelected      = lipgloss.NewStyle().Background(lipgloss.Color("62")).Foreground(lipgloss.Color("230")).Bold(true)
	styleHelp          = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	styleLabel         = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styleWarn          = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true)
	styleStateRunning   = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true)
	styleStateThinking  = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	styleStateDone      = lipgloss.NewStyle().Foreground(lipgloss.Color("71")).Bold(true)
	styleStateAsking    = lipgloss.NewStyle().Foreground(lipgloss.Color("135")).Bold(true)
	styleStatePlanReady = lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Bold(true)
	stylePreview       = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styleDivider       = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
)

// ── Git ───────────────────────────────────────────────────────────────────────

var gitCache = map[string]*GitInfo{}

func getGitInfo(dir string) *GitInfo {
	if dir == "" {
		return nil
	}
	if info, ok := gitCache[dir]; ok {
		return info
	}

	rootBytes, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		gitCache[dir] = nil
		return nil
	}
	root := strings.TrimSpace(string(rootBytes))

	if info, ok := gitCache[root]; ok {
		gitCache[dir] = info
		return info
	}

	branchBytes, _ := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	statusBytes, _ := exec.Command("git", "-C", dir, "status", "--porcelain").Output()

	mainRoot := root
	if wtOut, err := exec.Command("git", "-C", dir, "worktree", "list", "--porcelain").Output(); err == nil {
		for _, line := range strings.Split(string(wtOut), "\n") {
			if strings.HasPrefix(line, "worktree ") {
				mainRoot = strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
				break // first entry is always the main worktree
			}
		}
	}

	info := &GitInfo{
		Branch:   strings.TrimSpace(string(branchBytes)),
		Dirty:    len(strings.TrimSpace(string(statusBytes))) > 0,
		Root:     root,
		MainRoot: mainRoot,
		Name:     filepath.Base(mainRoot),
	}
	gitCache[dir] = info
	gitCache[root] = info
	return info
}

func gitTag(git *GitInfo) string {
	if git == nil || git.Branch == "" {
		return ""
	}
	s := " [" + git.Branch
	if git.Dirty {
		s += " *"
	}
	return s + "]"
}

// ── Agent state (via tmux-tap) ────────────────────────────────────────────────

func parseAgentState(data []byte) AgentState {
	switch strings.TrimSpace(string(data)) {
	case "running":
		return StateRunning
	case "thinking":
		return StateThinking
	case "plan_ready":
		return StatePlanReady
	case "asking":
		return StateAsking
	case "done":
		return StateDone
	default: // "inactive" or unknown
		return StateNone
	}
}

// ── Tmux queries ──────────────────────────────────────────────────────────────

func tmuxLines(args ...string) []string {
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		return nil
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "\n")
}

func fetchAllPanes() ([]Pane, error) {
	lines := tmuxLines("list-panes", "-a", "-F", "#{pane_id}|#{pane_pid}|#{pane_current_command}|#{pane_current_path}|#{pane_title}|#{@tap_state}|#{session_name}")
	if lines == nil {
		return nil, fmt.Errorf("could not list panes")
	}

	var panes []Pane
	for _, line := range lines {
		parts := strings.SplitN(line, "|", 7)
		if len(parts) < 7 {
			continue
		}
		p := Pane{
			ID:      parts[0],
			PID:     parts[1],
			Command: parts[2],
			Dir:     parts[3],
			Title:   parts[4],
			Session: parts[6],
			Git:     getGitInfo(parts[3]),
			Agent:   parseAgentState([]byte(parts[5])),
		}
		panes = append(panes, p)
	}
	return panes, nil
}

func capturePane(target string) string {
	if target == "" {
		return ""
	}
	out, err := exec.Command("tmux", "capture-pane", "-p", "-e", "-t", target).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// ── Grouping ──────────────────────────────────────────────────────────────────

func groupByRepo(panes []Pane) []RepoGroup {
	var order []string
	groups := map[string]*RepoGroup{}

	for _, p := range panes {
		key := "__no_repo__"
		if p.Git != nil {
			key = p.Git.MainRoot
		}
		if _, exists := groups[key]; !exists {
			name := "No project"
			if p.Git != nil {
				name = p.Git.Name
			}
			groups[key] = &RepoGroup{Name: name, Git: p.Git}
			order = append(order, key)
		}
		groups[key].Panes = append(groups[key].Panes, p)
	}

	result := make([]RepoGroup, 0, len(order))
	for _, key := range order {
		if key != "__no_repo__" {
			result = append(result, *groups[key])
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	if g, ok := groups["__no_repo__"]; ok {
		result = append([]RepoGroup{*g}, result...)
	}
	return result
}

func groupByDirectory(panes []Pane) []RepoGroup {
	var order []string
	groups := map[string]*RepoGroup{}

	for _, p := range panes {
		key := p.Dir
		if key == "" {
			key = "__unknown__"
		}
		if _, exists := groups[key]; !exists {
			name := shortDir(key)
			if key == "__unknown__" {
				name = "Unknown"
			}
			groups[key] = &RepoGroup{Name: name, Git: p.Git}
			order = append(order, key)
		}
		groups[key].Panes = append(groups[key].Panes, p)
	}

	result := make([]RepoGroup, 0, len(order))
	for _, key := range order {
		result = append(result, *groups[key])
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result
}

func groupBySessionName(panes []Pane) []RepoGroup {
	var order []string
	groups := map[string]*RepoGroup{}

	for _, p := range panes {
		key := p.Session
		if key == "" {
			key = "__no_session__"
		}
		if _, exists := groups[key]; !exists {
			name := key
			if key == "__no_session__" {
				name = "No session"
			}
			groups[key] = &RepoGroup{Name: name, Git: p.Git}
			order = append(order, key)
		}
		groups[key].Panes = append(groups[key].Panes, p)
	}

	result := make([]RepoGroup, 0, len(order))
	for _, key := range order {
		result = append(result, *groups[key])
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result
}

// ── Item building ─────────────────────────────────────────────────────────────

func shortDir(dir string) string {
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(dir, home) {
		rel := "~" + dir[len(home):]
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) > 3 {
			return "~" + string(filepath.Separator) + filepath.Join(parts[len(parts)-2], parts[len(parts)-1])
		}
		return rel
	}
	parts := strings.Split(dir, string(filepath.Separator))
	if len(parts) > 4 {
		return string(filepath.Separator) + filepath.Join(parts[len(parts)-2], parts[len(parts)-1])
	}
	return dir
}

func buildItems(groups []RepoGroup) []Item {
	var items []Item

	for _, g := range groups {
		items = append(items, Item{
			Kind:  KindRepo,
			Label: "◆ " + g.Name + gitTag(g.Git),
		})

		// Detect multiple worktree roots within this group
		var wtOrder []string
		wtPanes := map[string][]Pane{}
		for _, p := range g.Panes {
			root := ""
			if p.Git != nil {
				root = p.Git.Root
			}
			if _, exists := wtPanes[root]; !exists {
				wtOrder = append(wtOrder, root)
			}
			wtPanes[root] = append(wtPanes[root], p)
		}

		multiWT := len(wtOrder) > 1

		for _, root := range wtOrder {
			panes := wtPanes[root]
			panePrefix := "  "
			if multiWT {
				// Find the git info for this worktree to get its branch
				var wtGit *GitInfo
				if len(panes) > 0 {
					wtGit = panes[0].Git
				}
				wtName := filepath.Base(root)
				wtLabel := "  ◇ " + wtName
				if wtGit != nil {
					wtLabel += styleLabel.Render("  ["+wtGit.Branch+"]")
				}
				items = append(items, Item{Kind: KindWorktree, Label: wtLabel})
				panePrefix = "    "
			}

			for pi, p := range panes {
				prefix := panePrefix + "├ "
				if pi == len(panes)-1 {
					prefix = panePrefix + "└ "
				}
				label := prefix + shortDir(p.Dir) + "  " + styleLabel.Render("["+p.Command+"]")

				switch p.Agent {
				case StateRunning:
					label += "  " + styleStateRunning.Render("● running")
				case StateThinking:
					label += "  " + styleStateThinking.Render("◌ thinking")
				case StateDone:
					label += "  " + styleStateDone.Render("✓ done")
				case StateAsking:
					label += "  " + styleStateAsking.Render("? choose")
				case StatePlanReady:
					label += "  " + styleStatePlanReady.Render("📋 plan ready")
				}

				if p.Title != "" && p.Title != p.Command {
					label += "  " + styleLabel.Render(p.Title)
				}

				items = append(items, Item{
					Kind:   KindPane,
					Label:  label,
					Target: p.ID,
					Dir:    p.Dir,
					Agent:  p.Agent,
				})
			}
		}
	}
	return items
}

// ── Repo finder ───────────────────────────────────────────────────────────────

func findRepos() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	seen := map[string]bool{}
	var repos []string

	walk := func(root string, maxDepth int) {
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			rel, _ := filepath.Rel(root, path)
			if strings.Count(rel, string(filepath.Separator)) >= maxDepth {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
				if !seen[path] {
					seen[path] = true
					repos = append(repos, path)
				}
				return filepath.SkipDir
			}
			return nil
		})
	}

	// Scan home shallowly (e.g. ~/dotfiles), known subdirs more deeply
	walk(home, 2)
	for _, sub := range []string{"projects", "code", "dev", "src", "work", "repos"} {
		if p := filepath.Join(home, sub); dirExists(p) {
			walk(p, 4)
		}
	}

	return repos
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func filteredRepos(repos []string, filter string) []string {
	if filter == "" {
		return repos
	}
	filter = strings.ToLower(filter)
	var result []string
	for _, r := range repos {
		if strings.Contains(strings.ToLower(r), filter) {
			result = append(result, r)
		}
	}
	return result
}

// ── Pane filtering ────────────────────────────────────────────────────────

func filterAgentPanes(panes []Pane) []Pane {
	var out []Pane
	for _, p := range panes {
		if p.Agent != StateNone {
			out = append(out, p)
		}
	}
	return out
}

// ── Model ─────────────────────────────────────────────────────────────────────

type Model struct {
	items      []Item
	cursor     int
	offset     int
	err        error
	state      appState
	textInput  textinput.Model
	repos      []string
	repoCursor int
	width      int
	height     int
	preview    string
	// raw panes for kanban views
	panes      []Pane
	mode       viewMode
	// kanban repo board state
	kanbanCol    int            // absolute column index into repo groups
	kanbanColOff int            // first visible column index
	kanbanRow    map[string]int // repo key → row cursor
	kanbanOff    map[string]int // repo key → scroll offset
	// drill-down state
	drillRepo string
	drillCol  int
	drillRow  [3]int
	drillOff  [3]int
	// filter: show only panes with an active agent
	agentOnly bool
	// grouping mode
	grouping groupMode
}

func (m Model) effectivePanes() []Pane {
	if m.agentOnly {
		return filterAgentPanes(m.panes)
	}
	return m.panes
}

func (m Model) groupPanes() []RepoGroup {
	panes := m.effectivePanes()
	switch m.grouping {
	case groupByDir:
		return groupByDirectory(panes)
	case groupBySession:
		return groupBySessionName(panes)
	default:
		return groupByRepo(panes)
	}
}

func (m Model) groupModeLabel() string {
	switch m.grouping {
	case groupByDir:
		return "directory"
	case groupBySession:
		return "session"
	default:
		return "project"
	}
}

func initialModel() Model {
	panes, err := fetchAllPanes()
	agentOnly := loadAgentOnly()

	m := Model{
		panes:     panes,
		err:       err,
		kanbanRow: map[string]int{},
		kanbanOff: map[string]int{},
		agentOnly: agentOnly,
		grouping:  loadGroupMode(),
		mode:      loadViewMode(),
	}

	groups := m.groupPanes()
	items := buildItems(groups)
	m.items = items

	// Highlight the pane from which tmux-nav was launched
	activePaneID := ""
	if out, err2 := exec.Command("tmux", "display-message", "-p", "#{pane_id}").Output(); err2 == nil {
		activePaneID = strings.TrimSpace(string(out))
	}
	cursor := 0
	found := false
	for i, item := range items {
		if item.Kind == KindPane && item.Target == activePaneID {
			cursor = i
			found = true
			break
		}
	}
	if !found {
		for i, item := range items {
			if item.Kind == KindPane {
				cursor = i
				break
			}
		}
	}
	m.cursor = cursor

	// Sync kanban cursors to the active pane
	if activePaneID != "" {
		for colIdx, g := range groups {
			for rowIdx, p := range g.Panes {
				if p.ID == activePaneID {
					m.kanbanCol = colIdx
					key := repoKey(p)
					m.kanbanRow[key] = rowIdx
					break
				}
			}
		}
	}

	if cursor < len(items) {
		m.preview = capturePane(items[cursor].Target)
	}
	return m
}

type tickMsg struct{}

func (m Model) tickCmd() tea.Cmd {
	interval := 2 * time.Second
	for _, p := range m.panes {
		if p.Agent == StateRunning || p.Agent == StateThinking || p.Agent == StateAsking || p.Agent == StatePlanReady {
			interval = 200 * time.Millisecond
			break
		}
	}
	return tea.Tick(interval, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m Model) Init() tea.Cmd { return m.tickCmd() }

// softRefresh re-fetches pane states without clearing the git cache.
// Called on every tick to keep running/done badges live.
func (m *Model) softRefresh() {
	panes, err := fetchAllPanes()
	if err != nil {
		return
	}
	m.panes = panes
	groups := m.groupPanes()
	m.items = buildItems(groups)
	if m.cursor >= len(m.items) {
		m.cursor = max(0, len(m.items)-1)
	}
	m.clampScroll()
	if m.kanbanRow != nil {
		m.clampKanbanCursor()
	}
	if m.mode == viewKanbanDrill {
		m.clampDrillCursor()
	}
}

func (m *Model) refresh() {
	gitCache = map[string]*GitInfo{}
	panes, err := fetchAllPanes()
	m.err = err
	m.panes = panes
	groups := m.groupPanes()
	m.items = buildItems(groups)
	if m.cursor >= len(m.items) {
		m.cursor = max(0, len(m.items)-1)
	}
	m.clampScroll()
	m.preview = capturePane(m.currentTarget())
	if m.kanbanRow == nil {
		m.kanbanRow = map[string]int{}
		m.kanbanOff = map[string]int{}
	}
	m.clampKanbanCursor()
	if m.mode == viewKanbanDrill {
		m.clampDrillCursor()
	}
}

func (m Model) currentTarget() string {
	if m.cursor < len(m.items) {
		return m.items[m.cursor].Target
	}
	return ""
}

func (m Model) listHeight() int {
	h := m.height - 4 // title + blank + blank + help
	if h < 1 {
		return 1
	}
	return h
}

func (m *Model) clampScroll() {
	lh := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+lh {
		m.offset = m.cursor - lh + 1
	}
}

func (m *Model) moveCursor(delta int) {
	next := m.cursor + delta
	for next >= 0 && next < len(m.items) {
		if m.items[next].Kind == KindPane {
			m.cursor = next
			break
		}
		next += delta
	}
	m.clampScroll()
	m.preview = capturePane(m.currentTarget())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		m.softRefresh()
		if m.mode == viewList {
			m.preview = capturePane(m.currentTarget())
		}
		return m, m.tickCmd()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.clampScroll()

	case tea.KeyMsg:
		switch m.state {

		case stateConfirmKill:
			switch msg.String() {
			case "y", "Y":
				killed := m.items[m.cursor].Target
				// Switch to another pane first so tmux doesn't exit
				for _, item := range m.items {
					if item.Kind == KindPane && item.Target != killed {
						exec.Command("tmux", "switch-client", "-t", item.Target).Run()
						break
					}
				}
				exec.Command("tmux", "kill-pane", "-t", killed).Run()
				m.refresh()
				m.state = stateBrowsing
			default:
				m.state = stateBrowsing
			}

		case statePickRepo:
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc":
				m.state = stateBrowsing
			case "enter":
				visible := filteredRepos(m.repos, m.textInput.Value())
				if m.repoCursor < len(visible) {
					dir := visible[m.repoCursor]
					name := filepath.Base(dir)
					exec.Command("tmux", "new-session", "-d", "-s", name, "-c", dir).Run()
					exec.Command("tmux", "switch-client", "-t", name).Run()
					return m, tea.Quit
				}
			case "up":
				if m.repoCursor > 0 {
					m.repoCursor--
				}
			case "down":
				if visible := filteredRepos(m.repos, m.textInput.Value()); m.repoCursor < len(visible)-1 {
					m.repoCursor++
				}
			default:
				prev := m.textInput.Value()
				var cmd tea.Cmd
				m.textInput, cmd = m.textInput.Update(msg)
				if m.textInput.Value() != prev {
					m.repoCursor = 0
				}
				return m, cmd
			}

		case stateBrowsing:
			// Drill-down key handling (takes priority)
			if m.mode == viewKanbanDrill {
				switch msg.String() {
				case "q", "ctrl+c":
					return m, tea.Quit
				case "esc":
					m.mode = viewKanban
				case "h":
					if m.drillCol > 0 {
						m.drillCol--
					}
				case "l":
					if m.drillCol < 2 {
						m.drillCol++
					}
				case "j":
					cols := m.drillPanes()
					col := m.drillCol
					if m.drillRow[col] < len(cols[col])-1 {
						m.drillRow[col]++
						m.clampDrillCursor()
					}
				case "k":
					col := m.drillCol
					if m.drillRow[col] > 0 {
						m.drillRow[col]--
						m.clampDrillCursor()
					}
				case "enter":
					cols := m.drillPanes()
					col := m.drillCol
					if len(cols[col]) > 0 {
						target := cols[col][m.drillRow[col]].ID
						exec.Command("tmux", "switch-client", "-t", target).Run()
						return m, tea.Quit
					}
				}
				return m, nil
			}

			// Kanban repo board key handling
			if m.mode == viewKanban {
				groups := m.groupPanes()
				switch msg.String() {
				case "q", "esc", "ctrl+c":
					return m, tea.Quit
				case "a":
					m.agentOnly = !m.agentOnly
					m.refresh()
				case "g":
					m.grouping = (m.grouping + 1) % 3
					m.refresh()
				case "b":
					m.mode = viewList
					saveViewMode(m.mode)
					// sync list cursor to current kanban selection
					if m.kanbanCol < len(groups) {
						g := groups[m.kanbanCol]
						key := repoKey(g.Panes[0])
						if len(g.Panes) > 0 {
							row := m.kanbanRow[key]
							if row < len(g.Panes) {
								target := g.Panes[row].ID
								for i, item := range m.items {
									if item.Kind == KindPane && item.Target == target {
										m.cursor = i
										m.clampScroll()
										m.preview = capturePane(target)
										break
									}
								}
							}
						}
					}
				case "h":
					if m.kanbanCol > 0 {
						m.kanbanCol--
						m.clampKanbanCursor()
					}
				case "l":
					if m.kanbanCol < len(groups)-1 {
						m.kanbanCol++
						m.clampKanbanCursor()
					}
				case "j":
					if m.kanbanCol < len(groups) {
						g := groups[m.kanbanCol]
						if len(g.Panes) > 0 {
							key := repoKey(g.Panes[0])
							if m.kanbanRow[key] < len(g.Panes)-1 {
								m.kanbanRow[key]++
								m.clampKanbanCursor()
							}
						}
					}
				case "k":
					if m.kanbanCol < len(groups) {
						g := groups[m.kanbanCol]
						if len(g.Panes) > 0 {
							key := repoKey(g.Panes[0])
							if m.kanbanRow[key] > 0 {
								m.kanbanRow[key]--
								m.clampKanbanCursor()
							}
						}
					}
				case "tab":
					if m.kanbanCol < len(groups) {
						g := groups[m.kanbanCol]
						key := "__no_repo__"
						if g.Git != nil {
							key = g.Git.MainRoot
						}
						m.drillRepo = key
						m.drillCol = 0
						m.drillRow = [3]int{}
						m.drillOff = [3]int{}
						m.mode = viewKanbanDrill
					}
				case "enter":
					if m.kanbanCol < len(groups) {
						g := groups[m.kanbanCol]
						if len(g.Panes) > 0 {
							key := repoKey(g.Panes[0])
							row := m.kanbanRow[key]
							if row < len(g.Panes) {
								target := g.Panes[row].ID
								exec.Command("tmux", "switch-client", "-t", target).Run()
								return m, tea.Quit
							}
						}
					}
				}
				return m, nil
			}

			// List view key handling
			switch msg.String() {
			case "q", "esc", "ctrl+c":
				return m, tea.Quit
			case "b":
				m.mode = viewKanban
				saveViewMode(m.mode)
				// sync kanban cursor to current list selection
				if m.cursor < len(m.items) && m.items[m.cursor].Kind == KindPane {
					target := m.items[m.cursor].Target
					groups := m.groupPanes()
					for ci, g := range groups {
						for ri, p := range g.Panes {
							if p.ID == target {
								m.kanbanCol = ci
								key := repoKey(p)
								m.kanbanRow[key] = ri
								m.clampKanbanCursor()
							}
						}
					}
				}
			case "up", "k":
				m.moveCursor(-1)
			case "down", "j":
				m.moveCursor(1)
			case "x":
				if m.items[m.cursor].Kind == KindPane {
					m.state = stateConfirmKill
				}
			case "w":
				if dir := m.items[m.cursor].Dir; dir != "" {
					exec.Command("tmux", "new-window", "-c", dir).Run()
					return m, tea.Quit
				}
			case "t":
				item := m.items[m.cursor]
				if item.Kind == KindPane && item.Dir != "" {
					if git := getGitInfo(item.Dir); git != nil {
						home, _ := os.UserHomeDir()
						repoName := filepath.Base(git.Root)
						suffix := time.Now().Format("0102-1504")
						name := repoName + "-" + suffix
						worktreePath := filepath.Join(home, ".worktrees", name)
						exec.Command("git", "-C", git.Root, "worktree", "add", worktreePath, "-b", name).Run()
						exec.Command("tmux", "new-session", "-d", "-s", name, "-c", worktreePath).Run()
						exec.Command("tmux", "switch-client", "-t", name).Run()
						return m, tea.Quit
					}
				}
			case "n":
				m.repos = findRepos()
				m.repoCursor = 0
				ti := textinput.New()
				ti.Placeholder = "filter repos..."
				ti.Focus()
				ti.CharLimit = 64
				ti.Width = 40
				m.textInput = ti
				m.state = statePickRepo
			case "a":
				m.agentOnly = !m.agentOnly
				saveAgentOnly(m.agentOnly)
				m.refresh()
			case "g":
				m.grouping = (m.grouping + 1) % 3
				saveGroupMode(m.grouping)
				m.refresh()
			case "enter":
				if target := m.currentTarget(); target != "" {
					exec.Command("tmux", "switch-client", "-t", target).Run()
					return m, tea.Quit
				}
			}
		}
	}
	return m, nil
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.err != nil {
		return fmt.Sprintf("\n  Error: %v\n\n  Press q to quit.\n", m.err)
	}
	if m.state == statePickRepo {
		return m.viewRepoPicker()
	}
	if m.mode == viewKanban {
		return m.viewKanban()
	}
	if m.mode == viewKanbanDrill {
		return m.viewKanbanDrill()
	}
	if m.width == 0 {
		return ""
	}

	leftWidth := m.width / 2
	rightWidth := m.width - leftWidth - 1 // -1 for divider
	panelHeight := m.height - 1           // -1 for help bar

	left := lipgloss.NewStyle().Width(leftWidth).Height(panelHeight).Render(
		strings.TrimRight(m.renderLeft(), "\n"),
	)
	right := lipgloss.NewStyle().Width(rightWidth).Height(panelHeight).Render(
		strings.TrimRight(m.renderRight(rightWidth, panelHeight), "\n"),
	)

	divLines := make([]string, panelHeight)
	for i := range divLines {
		divLines[i] = styleDivider.Render("│")
	}
	divider := strings.Join(divLines, "\n")

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right)

	var helpBar string
	if m.state == stateConfirmKill {
		helpBar = styleWarn.Render("  Kill this pane? (y/n)")
	} else {
		helpBar = styleHelp.Render("  j/k  navigate   enter  select   a  toggle agents   g  group by   b  kanban   x  kill   w  new window   n  new session   t  new worktree   q  quit")
	}

	return body + "\n" + helpBar
}

func (m Model) renderLeft() string {
	var sb strings.Builder
	sb.WriteString("\n")
	title := "Tmux Navigator"
	title += "  " + styleLabel.Render("[by "+m.groupModeLabel()+"]")
	if m.agentOnly {
		title += "  " + styleLabel.Render("[agents only]")
	}
	sb.WriteString(styleTitle.Render(title) + "\n\n")

	maxW := m.width/2 - 1
	end := min(m.offset+m.listHeight(), len(m.items))
	for i := m.offset; i < end; i++ {
		item := m.items[i]
		line := " " + item.Label
		if i == m.cursor {
			sb.WriteString(styleSelected.MaxWidth(maxW).Render(line) + "\n")
		} else if item.Kind == KindRepo {
			sb.WriteString(styleRepo.MaxWidth(maxW).Render(line) + "\n")
		} else if item.Kind == KindWorktree {
			sb.WriteString(styleWorktree.MaxWidth(maxW).Render(line) + "\n")
		} else {
			sb.WriteString(stylePane.MaxWidth(maxW).Render(line) + "\n")
		}
	}
	return sb.String()
}

func (m Model) renderRight(width, height int) string {
	if m.preview == "" {
		return styleLabel.Render("\n  no preview")
	}

	cleaned := strings.ReplaceAll(m.preview, "\r", "")
	lines := strings.Split(strings.TrimRight(cleaned, "\n"), "\n")
	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}

	var sb strings.Builder
	for _, l := range lines {
		// Truncate using lipgloss so ANSI sequences aren't broken mid-sequence
		sb.WriteString(lipgloss.NewStyle().MaxWidth(width).Render(l) + "\n")
	}
	return sb.String()
}

func truncate(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxWidth {
		return s
	}
	if maxWidth < 2 {
		return string(runes[:maxWidth])
	}
	return string(runes[:maxWidth-1]) + "…"
}

func (m Model) viewRepoPicker() string {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(styleTitle.Render("New Session — Pick a repo") + "\n\n")
	sb.WriteString(styleLabel.Render("  ") + m.textInput.View() + "\n\n")

	home, _ := os.UserHomeDir()
	visible := filteredRepos(m.repos, m.textInput.Value())

	if len(visible) == 0 {
		sb.WriteString(stylePane.Render("  no repos found") + "\n")
	}
	for i, repo := range visible {
		line := "  " + strings.Replace(repo, home, "~", 1)
		if i == m.repoCursor {
			sb.WriteString(styleSelected.Render(line) + "\n")
		} else {
			sb.WriteString(stylePane.Render(line) + "\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(styleHelp.Render("  ↑/↓  navigate   enter  create session   esc  cancel") + "\n")
	return sb.String()
}


// ── Kanban helpers ────────────────────────────────────────────────────────────

func (m Model) kanbanNumVisible() int {
	n := (m.width + 1) / (kanbanMinColWidth + 1)
	if n < 1 {
		return 1
	}
	return n
}

func (m Model) kanbanColWidth() int {
	n := m.kanbanNumVisible()
	w := (m.width - (n - 1)) / n
	if w < 4 {
		return 4
	}
	return w
}

func (m Model) kanbanBodyHeight() int {
	h := m.height - 3 // title + help + 1 spare
	if h < 1 {
		return 1
	}
	return h
}

func (m *Model) clampKanbanCursor() {
	if m.kanbanRow == nil {
		m.kanbanRow = map[string]int{}
		m.kanbanOff = map[string]int{}
	}
	groups := m.groupPanes()
	if len(groups) == 0 {
		m.kanbanCol = 0
		m.kanbanColOff = 0
		return
	}
	if m.kanbanCol >= len(groups) {
		m.kanbanCol = len(groups) - 1
	}
	if m.kanbanCol < 0 {
		m.kanbanCol = 0
	}
	nVis := m.kanbanNumVisible()
	if m.kanbanColOff > m.kanbanCol {
		m.kanbanColOff = m.kanbanCol
	}
	if m.kanbanCol >= m.kanbanColOff+nVis {
		m.kanbanColOff = m.kanbanCol - nVis + 1
	}
	if m.kanbanColOff < 0 {
		m.kanbanColOff = 0
	}
	for _, g := range groups {
		if len(g.Panes) == 0 {
			continue
		}
		key := repoKey(g.Panes[0])
		n := len(g.Panes)
		if m.kanbanRow[key] >= n {
			m.kanbanRow[key] = n - 1
		}
		vis := m.kanbanBodyHeight() / kanbanCardHeight
		if vis < 1 {
			vis = 1
		}
		row := m.kanbanRow[key]
		if row < m.kanbanOff[key] {
			m.kanbanOff[key] = row
		}
		if row >= m.kanbanOff[key]+vis {
			m.kanbanOff[key] = row - vis + 1
		}
	}
}

func (m Model) drillPanes() [3][]Pane {
	var cols [3][]Pane
	for _, p := range m.effectivePanes() {
		if repoKey(p) == m.drillRepo {
			var col int
			switch p.Agent {
			case StateRunning, StateThinking:
				col = 1
			case StateDone:
				col = 2
			default: // StateNone, StateAsking, StatePlanReady
				col = 0
			}
			cols[col] = append(cols[col], p)
		}
	}
	return cols
}

func (m *Model) clampDrillCursor() {
	cols := m.drillPanes()
	if m.drillCol >= 3 {
		m.drillCol = 2
	}
	for c := 0; c < 3; c++ {
		n := len(cols[c])
		if n == 0 {
			m.drillRow[c] = 0
			m.drillOff[c] = 0
			continue
		}
		if m.drillRow[c] >= n {
			m.drillRow[c] = n - 1
		}
		vis := m.kanbanBodyHeight() / drillCardHeight
		if vis < 1 {
			vis = 1
		}
		row := m.drillRow[c]
		if row < m.drillOff[c] {
			m.drillOff[c] = row
		}
		if row >= m.drillOff[c]+vis {
			m.drillOff[c] = row - vis + 1
		}
	}
}

// ── Kanban card rendering ─────────────────────────────────────────────────────

func renderKanbanCard(p Pane, colW int, selected bool) string {
	contentW := colW - 4 // border(1+1) + padding(1+1)
	if contentW < 4 {
		contentW = 4
	}
	mw := lipgloss.NewStyle().MaxWidth(contentW)

	line1 := mw.Render(shortDir(p.Dir) + "  " + styleLabel.Render("["+p.Command+"]"))

	var parts []string
	if p.Git != nil && p.Git.Branch != "" {
		b := p.Git.Branch
		if p.Git.Dirty {
			b += " *"
		}
		parts = append(parts, styleLabel.Render(b))
	}
	switch p.Agent {
	case StateRunning:
		parts = append(parts, styleStateRunning.Render("● running"))
	case StateThinking:
		parts = append(parts, styleStateThinking.Render("◌ thinking"))
	case StateDone:
		parts = append(parts, styleStateDone.Render("✓ done"))
	case StateAsking:
		parts = append(parts, styleStateAsking.Render("? choose"))
	case StatePlanReady:
		parts = append(parts, styleStatePlanReady.Render("📋 plan ready"))
	}
	line2 := " "
	if len(parts) > 0 {
		line2 = mw.Render(strings.Join(parts, "  "))
	}

	borderColor := lipgloss.Color("238")
	if selected {
		borderColor = lipgloss.Color("62")
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(contentW).
		Padding(0, 1).
		Render(line1 + "\n" + line2)
}

func renderDrillCard(p Pane, colW int, selected bool) string {
	contentW := colW - 4
	if contentW < 4 {
		contentW = 4
	}
	mw := lipgloss.NewStyle().MaxWidth(contentW)

	line1 := mw.Render(shortDir(p.Dir) + "  " + styleLabel.Render("["+p.Command+"]"))

	var parts []string
	if p.Git != nil && p.Git.Branch != "" {
		b := p.Git.Branch
		if p.Git.Dirty {
			b += " *"
		}
		parts = append(parts, styleLabel.Render(b))
	}
	switch p.Agent {
	case StateRunning:
		parts = append(parts, styleStateRunning.Render("● running"))
	case StateThinking:
		parts = append(parts, styleStateThinking.Render("◌ thinking"))
	case StateDone:
		parts = append(parts, styleStateDone.Render("✓ done"))
	case StateAsking:
		parts = append(parts, styleStateAsking.Render("? choose"))
	case StatePlanReady:
		parts = append(parts, styleStatePlanReady.Render("📋 plan ready"))
	}
	line2 := " "
	if len(parts) > 0 {
		line2 = mw.Render(strings.Join(parts, "  "))
	}

	line3 := " "
	if p.Title != "" && p.Title != p.Command {
		line3 = mw.Render(styleLabel.Render(p.Title))
	}

	borderColor := lipgloss.Color("238")
	if selected {
		borderColor = lipgloss.Color("62")
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(contentW).
		Padding(0, 1).
		Render(line1 + "\n" + line2 + "\n" + line3)
}

// ── Kanban column rendering ───────────────────────────────────────────────────

func (m Model) renderKanbanCol(g RepoGroup, colIdx int, colW int) string {
	header := g.Name + gitTag(g.Git)
	headerStr := lipgloss.NewStyle().Width(colW).Bold(true).
		Foreground(lipgloss.Color("213")).
		Render(truncate(" "+header, colW))

	bodyH := m.kanbanBodyHeight()
	vis := bodyH / kanbanCardHeight
	if vis < 1 {
		vis = 1
	}

	var key string
	if len(g.Panes) > 0 {
		key = repoKey(g.Panes[0])
	} else {
		key = "__no_repo__"
		if g.Git != nil {
			key = g.Git.MainRoot
		}
	}

	off := m.kanbanOff[key]
	row := m.kanbanRow[key]
	end := min(off+vis, len(g.Panes))

	var cards []string
	for i := off; i < end; i++ {
		selected := m.mode == viewKanban && colIdx == m.kanbanCol && i == row
		cards = append(cards, renderKanbanCard(g.Panes[i], colW, selected))
	}

	body := lipgloss.NewStyle().Width(colW).Height(bodyH).Render(strings.Join(cards, "\n"))
	return lipgloss.JoinVertical(lipgloss.Left, headerStr, body)
}

func (m Model) renderDrillCol(col int, panes []Pane, colW int) string {
	var headerColor lipgloss.Color
	switch col {
	case 1:
		headerColor = lipgloss.Color("208")
	case 2:
		headerColor = lipgloss.Color("71")
	default:
		headerColor = lipgloss.Color("245")
	}
	name := fmt.Sprintf("%s (%d)", drillColNames[col], len(panes))
	headerStr := lipgloss.NewStyle().Width(colW).Bold(true).
		Foreground(headerColor).Align(lipgloss.Center).Render(name)

	bodyH := m.kanbanBodyHeight()
	vis := bodyH / drillCardHeight
	if vis < 1 {
		vis = 1
	}

	off := m.drillOff[col]
	row := m.drillRow[col]
	end := min(off+vis, len(panes))

	var cards []string
	for i := off; i < end; i++ {
		selected := m.mode == viewKanbanDrill && col == m.drillCol && i == row
		cards = append(cards, renderDrillCard(panes[i], colW, selected))
	}

	body := lipgloss.NewStyle().Width(colW).Height(bodyH).Render(strings.Join(cards, "\n"))
	return lipgloss.JoinVertical(lipgloss.Left, headerStr, body)
}

// ── Kanban views ──────────────────────────────────────────────────────────────

func (m Model) viewKanban() string {
	if m.width == 0 {
		return ""
	}
	groups := m.groupPanes()
	if len(groups) == 0 {
		return styleLabel.Render("\n  No panes found.\n") + "\n" +
			styleHelp.Render("  b  list view   q  quit")
	}

	nVis := m.kanbanNumVisible()
	colW := m.kanbanColWidth()

	kanbanTitle := "Kanban Board"
	kanbanTitle += "  " + styleLabel.Render("[by "+m.groupModeLabel()+"]")
	if m.agentOnly {
		kanbanTitle += "  " + styleLabel.Render("[agents only]")
	}
	titleStr := styleTitle.Render(kanbanTitle)
	if m.kanbanColOff > 0 {
		titleStr += "  " + styleDivider.Render("◀ more")
	}
	end := m.kanbanColOff + nVis
	if end < len(groups) {
		titleStr += "  " + styleDivider.Render("more ▶")
	}
	if end > len(groups) {
		end = len(groups)
	}

	var colStrs []string
	for i := m.kanbanColOff; i < end; i++ {
		colStrs = append(colStrs, m.renderKanbanCol(groups[i], i, colW))
	}

	divH := m.kanbanBodyHeight() + 1
	divLines := make([]string, divH)
	for i := range divLines {
		divLines[i] = styleDivider.Render("│")
	}
	div := strings.Join(divLines, "\n")

	parts := []string{colStrs[0]}
	for i := 1; i < len(colStrs); i++ {
		parts = append(parts, div, colStrs[i])
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, parts...)

	helpBar := styleHelp.Render("  h/l  column   j/k  card   tab  drill in   enter  select   a  toggle agents   g  group by   b  list view   q  quit")
	return titleStr + "\n" + body + "\n" + helpBar
}

func (m Model) viewKanbanDrill() string {
	if m.width == 0 {
		return ""
	}

	repoDisplayName := "No project"
	for _, p := range m.effectivePanes() {
		if repoKey(p) == m.drillRepo {
			if p.Git != nil {
				repoDisplayName = p.Git.Name + gitTag(p.Git)
			}
			break
		}
	}

	titleStr := styleTitle.Render("◆ " + repoDisplayName + "  —  Pipeline")
	cols := m.drillPanes()

	colW := (m.width - 2) / 3 // 2 dividers
	if colW < 4 {
		colW = 4
	}

	col0 := m.renderDrillCol(0, cols[0], colW)
	col1 := m.renderDrillCol(1, cols[1], colW)
	col2 := m.renderDrillCol(2, cols[2], colW)

	divH := m.kanbanBodyHeight() + 1
	divLines := make([]string, divH)
	for i := range divLines {
		divLines[i] = styleDivider.Render("│")
	}
	div := strings.Join(divLines, "\n")

	body := lipgloss.JoinHorizontal(lipgloss.Top, col0, div, col1, div, col2)
	helpBar := styleHelp.Render("  h/l  column   j/k  card   enter  select   esc  back   q  quit")
	return titleStr + "\n" + body + "\n" + helpBar
}

// ── Entry point ───────────────────────────────────────────────────────────────

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
