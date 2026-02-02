package cli

import (
	"github.com/fatih/color"
)

// Color functions for consistent styling across commands
var (
	// Keywords (func, class, interface, type)
	Keyword = color.New(color.FgCyan, color.Bold).SprintFunc()
	
	// Symbol names
	Symbol = color.New(color.FgYellow).SprintFunc()
	
	// Types
	Type = color.New(color.FgGreen).SprintFunc()
	
	// File paths
	Path = color.New(color.FgHiBlack).SprintFunc()
	
	// Success indicators
	Success = color.New(color.FgGreen).SprintFunc()
	
	// Warning/error
	Warning = color.New(color.FgYellow).SprintFunc()
	Error   = color.New(color.FgRed).SprintFunc()
	
	// Info/counts
	Info = color.New(color.FgBlue).SprintFunc()
	
	// Bold for emphasis
	Bold = color.New(color.Bold).SprintFunc()
	
	// Dim for secondary info
	Dim = color.New(color.Faint).SprintFunc()
)
