package tui

import "charm.land/lipgloss/v2"

var (
	colorGreen  = lipgloss.Color("#51bd73")
	colorYellow = lipgloss.Color("#e5c07b")
	colorRed    = lipgloss.Color("#e06c75")
	colorGray   = lipgloss.Color("#5c6370")
	colorDim    = lipgloss.Color("#4b5263")
	colorWhite  = lipgloss.Color("#abb2bf")
	colorAccent = lipgloss.Color("#61afef")
	colorBorder = lipgloss.Color("#3e4452")
)

var (
	paneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder)

	paneTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWhite).
			PaddingLeft(1)

	sessionNameStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorWhite)

	sessionNameSelectedStyle = lipgloss.NewStyle().
					Bold(true).
					Foreground(colorAccent)

	sessionMetaStyle = lipgloss.NewStyle().
				Foreground(colorGray).
				PaddingLeft(2)

	detailLabelStyle = lipgloss.NewStyle().
				Foreground(colorGray)

	detailValueStyle = lipgloss.NewStyle().
				Foreground(colorWhite)

	detailSectionStyle = lipgloss.NewStyle().
				Foreground(colorDim).
				Bold(true)

	activityTimeStyle = lipgloss.NewStyle().
				Foreground(colorDim).
				Width(7)

	activityToolStyle = lipgloss.NewStyle().
				Foreground(colorAccent)

	activityDetailStyle = lipgloss.NewStyle().
				Foreground(colorGray)

	healthGreenStyle  = lipgloss.NewStyle().Foreground(colorGreen)
	healthYellowStyle = lipgloss.NewStyle().Foreground(colorYellow)
	healthRedStyle    = lipgloss.NewStyle().Foreground(colorRed)

	diagnosticWarningStyle  = lipgloss.NewStyle().Foreground(colorYellow)
	diagnosticCriticalStyle = lipgloss.NewStyle().Foreground(colorRed)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorDim).
			PaddingLeft(1)

	statusBarKeyStyle = lipgloss.NewStyle().
				Foreground(colorGray)
)
