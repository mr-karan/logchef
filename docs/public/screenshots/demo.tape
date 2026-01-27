# LogChef CLI Demo
Output cli-demo.webm
Output cli-demo.gif

Set Shell "fish"
Set FontSize 20
Set Width 1100
Set Height 700
Set Theme "MaterialOcean"
Set Framerate 60
Set Padding 20
Set TypingSpeed 40ms

# Version
Type "logchef --version"
Enter
Sleep 1s

# Interactive mode
Type "logchef query"
Enter
Sleep 1.5s

# Select team (Dev Team is first, just press Enter)
Enter
Sleep 1.5s

# Select source (HTTP Access Logs is first)
Enter
Sleep 1.5s

# Type query at the prompt
Type@60ms `status>=500`
Enter
Sleep 3s

# Direct command with pipe operator
Type@60ms `logchef query 'status>=400 | timestamp method status' -t "Dev Team" -S "HTTP Access Logs" --limit 5`
Enter
Sleep 3s

# SQL aggregation
Type@60ms `logchef sql "SELECT method, count() cnt FROM default.http GROUP BY method" -t "Dev Team" -S "HTTP Access Logs"`
Enter
Sleep 3s

# End
Type "# https://logchef.app/integration/cli"
Enter
Sleep 1.5s
