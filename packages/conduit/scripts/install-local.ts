import {
  appendFile,
  chmod,
  copyFile,
  mkdir,
  readFile,
  stat,
} from "node:fs/promises";
import { homedir } from "node:os";
import { basename, delimiter, dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const targets = ["linux-x64", "linux-arm64", "darwin-arm64", "windows-x64"];
const args = process.argv.slice(2);
if (args[0] === "--") args.shift();

const usage = `Usage: pnpm install:local [target]
Targets: ${targets.join(", ")}
Defaults to the current platform and architecture.
Build the selected target with pnpm build:standalone <target> first.`;

function shellQuote(value: string): string {
  return `'${value.replaceAll("'", "'\\''")}'`;
}

async function configurePath(directory: string): Promise<void> {
  if (
    (process.env.PATH ?? "")
      .split(delimiter)
      .some((entry) => entry !== "" && resolve(entry) === resolve(directory))
  ) {
    return;
  }

  const shell = basename(process.env.SHELL ?? "");
  const exportCommand = `export PATH=${shellQuote(directory)}:"$PATH"`;
  const configFiles =
    shell === "zsh"
      ? [join(process.env.ZDOTDIR || homedir(), ".zshrc")]
      : shell === "bash"
        ? [join(homedir(), ".bashrc")]
        : [];

  if (shell === "bash") {
    const loginFiles = [".bash_profile", ".bash_login", ".profile"];
    let loginFile = ".profile";
    for (const candidate of loginFiles) {
      if (
        await stat(join(homedir(), candidate)).then(
          () => true,
          () => false,
        )
      ) {
        loginFile = candidate;
        break;
      }
    }
    configFiles.push(join(homedir(), loginFile));
  }

  if (configFiles.length === 0) {
    console.log(
      `Add ${directory} to your shell's PATH before running conduit.`,
    );
    if (process.platform !== "win32") {
      console.log(
        shell === "fish"
          ? `Run: fish_add_path ${shellQuote(directory)}`
          : `For a POSIX shell, run: ${exportCommand}`,
      );
    }
    return;
  }

  for (const configFile of configFiles) {
    try {
      const contents = await readFile(configFile, "utf8").catch(
        (error: unknown) => {
          if (
            error instanceof Error &&
            "code" in error &&
            error.code === "ENOENT"
          ) {
            return "";
          }
          throw error;
        },
      );
      const block = `# Conduit local binary\ncase ":$PATH:" in\n  *:${shellQuote(directory)}:*) ;;\n  *) ${exportCommand} ;;\nesac\n`;
      if (!contents.includes(block)) {
        await mkdir(dirname(configFile), { recursive: true });
        await appendFile(configFile, `\n${block}`);
      }
      console.log(`PATH configured in ${configFile}`);
    } catch (error) {
      console.warn(
        `Could not configure PATH in ${configFile}: ${error instanceof Error ? error.message : String(error)}`,
      );
    }
  }
  console.log(
    `Open a new terminal, or run this in the current terminal:\n${exportCommand}`,
  );
}

try {
  if (args.length === 1 && ["--help", "-h"].includes(args[0]!)) {
    console.log(usage);
  } else {
    const platform =
      process.platform === "win32" ? "windows" : process.platform;
    const target = args[0] ?? `${platform}-${process.arch}`;
    if (args.length > 1 || !targets.includes(target)) {
      throw new Error(
        `Unsupported target or arguments: ${args.join(" ") || target}\n${usage}`,
      );
    }

    const extension = target === "windows-x64" ? ".exe" : "";
    const source = fileURLToPath(
      new URL(`../dist/release/conduit-${target}${extension}`, import.meta.url),
    );
    const sourceStat = await stat(source).catch(() => undefined);
    if (!sourceStat?.isFile()) {
      throw new Error(
        `Binary not found: ${source}\nRun pnpm build:standalone ${target} first.`,
      );
    }

    const destination = join(homedir(), ".local", "bin", `conduit${extension}`);
    await mkdir(dirname(destination), { recursive: true });
    await copyFile(source, destination);
    await chmod(destination, 0o755);
    console.log(`Installed ${target} to ${destination}`);
    await configurePath(dirname(destination));
  }
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
}
