package util

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// Help styles using lipgloss
var (
	// Professional and modern color palette
	lightGreen  = lipgloss.Color("#90EE90") // Soft light green
	gray        = lipgloss.Color("#A9A9A9") // Medium gray
	darkGray    = lipgloss.Color("#5A5A5A") // Dark gray for details
	brightGreen = lipgloss.Color("#00FF7F") // Bright green for highlights
	blue        = lipgloss.Color("#6366F1") // Modern blue (matches logger prefix)

	// Text styles
	titleStyle = lipgloss.NewStyle().
			Foreground(blue). // Title in blue (matching GonimeId prefix)
			Bold(true).
			PaddingBottom(1).
			MarginLeft(2)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(gray).
			Italic(true).
			PaddingBottom(1).
			MarginLeft(2)

	sectionTitleStyle = lipgloss.NewStyle().
				Foreground(lightGreen). // Section titles in light green
				Bold(true).
				PaddingLeft(2)

	commandStyle = lipgloss.NewStyle().
			Foreground(brightGreen). // Commands in bright green
			Bold(true).
			PaddingLeft(4)

	optionStyle = lipgloss.NewStyle().
			Foreground(brightGreen). // Options in bright green
			Bold(true).
			PaddingLeft(4)

	parameterStyle = lipgloss.NewStyle().
			Foreground(gray). // Parameters in gray to differentiate
			Italic(true)

	descriptionStyle = lipgloss.NewStyle().
				Foreground(gray). // Descriptions in gray
				PaddingLeft(6).
				Width(80 - 6) // Adjust width for line wrapping

	exampleStyle = lipgloss.NewStyle().
			Foreground(darkGray). // Examples in dark gray
			Italic(true).
			PaddingLeft(8)

	separatorStyle = lipgloss.NewStyle().
			Foreground(darkGray) // Separators in dark gray
)

// ShowBeautifulHelp displays a beautifully formatted help message
func ShowBeautifulHelp() {
	var helpContent strings.Builder

	// Program title
	helpContent.WriteString(titleStyle.Render("GonimeId - Anime Sub Indo dari Terminal"))
	helpContent.WriteString("\n")
	helpContent.WriteString(subtitleStyle.Render("Search, stream and download Indonesian-subtitled anime (Otakudesu, Samehadaku, Nimegami, YLnime, Moenime, Astronime) with mpv."))
	helpContent.WriteString("\n\n")

	// Usage section
	helpContent.WriteString(separatorStyle.Render(strings.Repeat("─", 80)))
	helpContent.WriteString("\n")
	helpContent.WriteString(sectionTitleStyle.Render("Usage:"))
	helpContent.WriteString("\n")
	helpContent.WriteString(commandStyle.Render("  gonimeid"))
	helpContent.WriteString("\n")
	helpContent.WriteString(descriptionStyle.Render("    Interactive mode - search and select anime from a beautiful menu"))
	helpContent.WriteString("\n")
	helpContent.WriteString(commandStyle.Render("  gonimeid "))
	helpContent.WriteString(parameterStyle.Render("[options]"))
	helpContent.WriteString("\n")
	helpContent.WriteString(descriptionStyle.Render("    Run with specific options"))
	helpContent.WriteString("\n")
	helpContent.WriteString(commandStyle.Render("  gonimeid "))
	helpContent.WriteString(parameterStyle.Render("[options] [anime name]"))
	helpContent.WriteString("\n")
	helpContent.WriteString(descriptionStyle.Render("    Direct search for anime (use spaces, not hyphens)"))
	helpContent.WriteString("\n")
	helpContent.WriteString(exampleStyle.Render("Example: gonimeid \"one piece\" (not \"one-piece\")"))
	helpContent.WriteString("\n\n")

	// Options section
	helpContent.WriteString(separatorStyle.Render(strings.Repeat("─", 80)))
	helpContent.WriteString("\n")
	helpContent.WriteString(sectionTitleStyle.Render("Options:"))
	helpContent.WriteString("\n")
	addOption(&helpContent, "--debug", "Enable debug mode for detailed error information and performance metrics. Logs are saved to a file for easy sharing.")
	addOption(&helpContent, "--perf", "Enable performance profiling - shows timing metrics for all operations.")
	addOption(&helpContent, "--help / -h", "Display this beautiful help message with detailed usage information.")
	addOption(&helpContent, "--version", "Show version information and build details.")
	addOption(&helpContent, "--update", "Check for updates and update automatically to the latest version.")
	addOption(&helpContent, "-d", "Download mode - download specific episodes for offline viewing.")
	addOption(&helpContent, "-r", "Range download mode - download multiple episodes (use with -d).")
	addOption(&helpContent, "-a", "Download ALL episodes (use with -d).")
	addOption(&helpContent, "--source", "Search one source only: otakudesu, samehadaku, nimegami, ylnime, moenime or astronime. Default: all.")
	addOption(&helpContent, "--quality", "Default download quality (1080p, 720p, 480p, 360p). Interactive playback always asks per source.")
	addOption(&helpContent, "-o", "Output directory for downloads (default: ~/.local/gonimeid/downloads/anime/). Files use Plex naming: Anime - S01E01.mp4.")
	helpContent.WriteString("\n")

	// Upscale Options section
	helpContent.WriteString(separatorStyle.Render(strings.Repeat("─", 80)))
	helpContent.WriteString("\n")
	helpContent.WriteString(sectionTitleStyle.Render("Upscale Options (Anime4K):"))
	helpContent.WriteString("\n")
	addOption(&helpContent, "--upscale", "Upscale mode - enhance video/image quality using Anime4K algorithm.")
	addOption(&helpContent, "--upscale-output", "Output path for upscaled file (default: input_upscaled.ext).")
	addOption(&helpContent, "--upscale-scale", "Upscale factor (1-4, default: 2x).")
	addOption(&helpContent, "--upscale-passes", "Number of processing passes (1-8, default: 2).")
	addOption(&helpContent, "--upscale-fast", "Use fast mode (lower quality but faster processing).")
	addOption(&helpContent, "--upscale-hq", "Use high quality mode (slower but better results).")
	addOption(&helpContent, "--upscale-gpu", "Use GPU encoding for video output (if available).")
	addOption(&helpContent, "--upscale-bitrate", "Video bitrate for output (default: 8M).")
	addOption(&helpContent, "--upscale-workers", "Number of parallel workers (default: CPU cores).")
	helpContent.WriteString("\n")

	// Features section
	helpContent.WriteString(separatorStyle.Render(strings.Repeat("─", 80)))
	helpContent.WriteString("\n")
	helpContent.WriteString(sectionTitleStyle.Render("Features:"))
	helpContent.WriteString("\n")

	addFeature(&helpContent, "Indonesian Sources", "Otakudesu, Samehadaku, Nimegami, YLnime, Moenime and Astronime, searched together; dead mirrors are skipped automatically.")
	addFeature(&helpContent, "Smart Search", "Intelligent search with fuzzy matching and suggestions.")
	addFeature(&helpContent, "Quality Selection", "Pick a resolution before every interactive playback; higher-quality download files are preferred over streaming mirrors.")
	addFeature(&helpContent, "Batch Downloads", "Download single episodes, ranges, or entire seasons for offline viewing.")
	addFeature(&helpContent, "Interactive Controls", "Beautiful terminal interface with keyboard navigation.")
	addFeature(&helpContent, "Discord Rich Presence", "Show your friends what you're watching.")
	addFeature(&helpContent, "Progress Tracking", "Keep track of your watch progress and episode history.")
	addFeature(&helpContent, "Skip Intros", "Automatically skip anime intros and outros.")
	addFeature(&helpContent, "Anime4K Upscaling", "Enhance video and image quality using the Anime4K algorithm.")
	helpContent.WriteString("\n")

	// Examples section
	helpContent.WriteString(separatorStyle.Render(strings.Repeat("─", 80)))
	helpContent.WriteString("\n")
	helpContent.WriteString(sectionTitleStyle.Render("Examples:"))
	helpContent.WriteString("\n")
	addExample(&helpContent, "gonimeid", "Start interactive mode")
	addExample(&helpContent, "gonimeid \"attack on titan\"", "Search directly for Attack on Titan")
	addExample(&helpContent, "gonimeid --debug \"naruto\"", "Search with debug information")
	addExample(&helpContent, "gonimeid --update", "Check for updates and update automatically")
	addExample(&helpContent, "gonimeid --version", "Show version information")
	addExample(&helpContent, "gonimeid -d \"one piece\" 1", "Download episode 1 of One Piece")
	addExample(&helpContent, "gonimeid -d -r \"naruto\" 1-5", "Download episodes 1-5 of Naruto")
	addExample(&helpContent, "gonimeid -d --source samehadaku \"bleach\" 10", "Download from Samehadaku only")
	addExample(&helpContent, "gonimeid -d --quality 720p \"demon slayer\" 1", "Download in 720p quality")
	addExample(&helpContent, "gonimeid -d --source otakudesu --quality 1080p \"jujutsu kaisen\" 5", "Otakudesu at 1080p")
	addExample(&helpContent, "gonimeid -d -a \"one piece\"", "Download ALL episodes of One Piece")
	addExample(&helpContent, "gonimeid -d -o ~/Anime \"one piece\" 1", "Download to custom directory with Plex naming")
	addExample(&helpContent, "gonimeid -d -r -o /media/anime \"naruto\" 1-12", "Download range to custom directory")
	helpContent.WriteString("\n")

	// Upscale Examples section
	helpContent.WriteString(separatorStyle.Render(strings.Repeat("─", 80)))
	helpContent.WriteString("\n")
	helpContent.WriteString(sectionTitleStyle.Render("Upscale Examples (Anime4K):"))
	helpContent.WriteString("\n")
	addExample(&helpContent, "gonimeid --upscale video.mp4", "Upscale video to 2x resolution")
	addExample(&helpContent, "gonimeid --upscale image.png", "Upscale image to 2x resolution")
	addExample(&helpContent, "gonimeid --upscale --upscale-hq video.mp4", "High quality upscale (4 passes)")
	addExample(&helpContent, "gonimeid --upscale --upscale-fast video.mp4", "Fast upscale (lower quality)")
	addExample(&helpContent, "gonimeid --upscale --upscale-scale 4 video.mp4", "Upscale to 4x resolution")
	addExample(&helpContent, "gonimeid --upscale --upscale-output out.mp4 video.mp4", "Specify output path")
	addExample(&helpContent, "gonimeid --upscale --upscale-gpu video.mp4", "Use GPU hardware encoding")
	helpContent.WriteString("\n")

	// Footer
	helpContent.WriteString(separatorStyle.Render(strings.Repeat("─", 80)))
	helpContent.WriteString("\n")
	helpContent.WriteString(subtitleStyle.Render("For more information, visit: https://github.com/KidiXDev/GonimeId"))
	helpContent.WriteString("\n")
	helpContent.WriteString(subtitleStyle.Render("Made with love for anime lovers everywhere"))
	helpContent.WriteString("\n\n")

	// Print the complete help content
	fmt.Print(helpContent.String())
}

// Helper functions for building help content
func addOption(builder *strings.Builder, opt, desc string) {
	builder.WriteString(optionStyle.Render("  " + opt))
	builder.WriteString("\n")
	builder.WriteString(descriptionStyle.Render("    " + desc))
	builder.WriteString("\n")
}

func addFeature(builder *strings.Builder, feature, desc string) {
	builder.WriteString(commandStyle.Render("  " + feature))
	builder.WriteString("\n")
	builder.WriteString(descriptionStyle.Render("    " + desc))
	builder.WriteString("\n")
}

func addExample(builder *strings.Builder, cmd, desc string) {
	builder.WriteString(commandStyle.Render("  " + cmd))
	builder.WriteString("\n")
	builder.WriteString(descriptionStyle.Render("    " + desc))
	builder.WriteString("\n")
}
