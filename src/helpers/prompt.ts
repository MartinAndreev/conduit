import { createInterface } from "node:readline";

export type PromptFn = (question: string) => Promise<string>;

export async function defaultPrompt(question: string): Promise<string> {
  const rl = createInterface({
    input: process.stdin,
    output: process.stderr,
  });
  return new Promise<string>((resolve) => {
    rl.question(question, (answer) => {
      rl.close();
      resolve(answer.trim());
    });
  });
}

export async function confirmYesNo(
  prompt: PromptFn,
  question: string,
): Promise<boolean> {
  const answer = await prompt(`${question} [y/N] `);
  return answer.toLowerCase() === "y" || answer.toLowerCase() === "yes";
}
