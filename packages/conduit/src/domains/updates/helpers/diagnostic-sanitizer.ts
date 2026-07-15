const SECRET_LINE =
  /(authorization|bearer|password|passwd|secret|token|api[_-]?key|credential|cookie)/i;

function removeControlCharacters(value: string): string {
  return Array.from(value)
    .filter((character) => {
      const code = character.charCodeAt(0);
      return (
        (code > 31 || code === 9 || code === 10 || code === 13) && code !== 127
      );
    })
    .join("");
}

export function sanitizeProcessDiagnostic(
  value: string,
  maximumCharacters = 2_000,
): string {
  const sanitized = removeControlCharacters(value)
    .split(/\r?\n/)
    .filter((line) => !SECRET_LINE.test(line))
    .join("\n")
    .trim();
  return sanitized.slice(0, maximumCharacters);
}
