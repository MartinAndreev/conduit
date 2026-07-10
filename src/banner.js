export const conduitBanner = String.raw`
   ____                _       _ _
  / ___|___  _ __   __| |_   _(_) |_
 | |   / _ \| '_ \ / _\` | | | | | __|
 | |__| (_) | | | | (_| | |_| | | |_
  \____\___/|_| |_|\__,_|\__,_|_|\__|
`;

export function shouldShowBanner(args) {
  const command = args[0];
  return (
    Boolean(process.stdout.isTTY) &&
    process.env.CONDUIT_NO_BANNER !== "1" &&
    ![undefined, "help", "--help", "-h", "version", "--version", "-V"].includes(
      command,
    )
  );
}
