use anyhow::Result;
use clap::{Args, Subcommand};
use include_dir::{Dir, include_dir};
use serde::Serialize;

/// The Logchef skill, embedded at compile time so its content always matches
/// the installed CLI version. The files live INSIDE the crate (`skill/`) rather
/// than being pulled from the repo-root `.agents/skills/logchef` — an embed path
/// that escapes the Cargo workspace breaks `cross` release builds, which mount
/// only the workspace. `scripts/sync-skill.sh` + CI keep the two copies in sync.
static SKILL_DIR: Dir<'_> = include_dir!("$CARGO_MANIFEST_DIR/skill");

/// Name of the single skill exposed by the CLI, mapped to `SKILL_DIR`.
const CORE_SKILL: &str = "core";

/// One-line description surfaced by `skills list`.
const CORE_DESCRIPTION: &str =
    "Logchef CLI usage, LogchefQL/SQL/LogsQL query syntax, and log investigation workflows";

#[derive(Args)]
#[command(after_help = "EXAMPLES:
  # List the bundled skills
  logchef skills list

  # Print the core usage guide (LogchefQL/SQL/LogsQL, workflows)
  logchef skills get core

  # Include every reference file (full syntax reference)
  logchef skills get core --full

  # Feed the guide to an agent as JSON
  logchef skills get core --json")]
pub struct SkillsArgs {
    #[command(subcommand)]
    command: Option<SkillsCommand>,

    /// Output machine-readable JSON.
    #[arg(long, global = true)]
    json: bool,
}

#[derive(Subcommand)]
enum SkillsCommand {
    /// List available skills.
    List,

    /// Print a skill's content.
    Get {
        /// Skill name (e.g. `core`).
        name: String,

        /// Append every reference file under `references/`.
        #[arg(long)]
        full: bool,
    },
}

#[derive(Serialize)]
struct SkillListEntry {
    name: &'static str,
    description: &'static str,
}

#[derive(Serialize)]
struct SkillGetOutput {
    name: String,
    content: String,
    references: std::collections::BTreeMap<String, String>,
}

pub async fn run(args: SkillsArgs) -> Result<()> {
    match args.command {
        None | Some(SkillsCommand::List) => list(args.json),
        Some(SkillsCommand::Get { name, full }) => get(&name, full, args.json),
    }
}

fn list(json: bool) -> Result<()> {
    let entries = [SkillListEntry {
        name: CORE_SKILL,
        description: CORE_DESCRIPTION,
    }];

    if json {
        println!("{}", serde_json::to_string_pretty(&entries)?);
    } else {
        for entry in &entries {
            println!("{}\t{}", entry.name, entry.description);
        }
    }

    Ok(())
}

fn get(name: &str, full: bool, json: bool) -> Result<()> {
    if name != CORE_SKILL {
        anyhow::bail!("Unknown skill '{}'. Valid skills: {}", name, CORE_SKILL);
    }

    let content = SKILL_DIR
        .get_file("SKILL.md")
        .and_then(|file| file.contents_utf8())
        .ok_or_else(|| anyhow::anyhow!("Embedded skill is missing SKILL.md"))?;

    if json {
        let output = SkillGetOutput {
            name: name.to_string(),
            content: content.to_string(),
            references: reference_files().collect(),
        };
        println!("{}", serde_json::to_string_pretty(&output)?);
        return Ok(());
    }

    print!("{}", content);
    if !content.ends_with('\n') {
        println!();
    }

    if full {
        for (path, body) in reference_files() {
            println!("\n---\n# references/{}\n", path);
            print!("{}", body);
            if !body.ends_with('\n') {
                println!();
            }
        }
    }

    Ok(())
}

/// Returns the embedded reference files as `(file_name, content)` pairs,
/// sorted by name for deterministic output.
fn reference_files() -> impl Iterator<Item = (String, String)> {
    let mut files: Vec<(String, String)> = SKILL_DIR
        .get_dir("references")
        .into_iter()
        .flat_map(|dir| dir.files())
        .filter_map(|file| {
            let name = file.path().file_name()?.to_string_lossy().into_owned();
            let body = file.contents_utf8()?.to_string();
            Some((name, body))
        })
        .collect();
    files.sort_by(|a, b| a.0.cmp(&b.0));
    files.into_iter()
}
