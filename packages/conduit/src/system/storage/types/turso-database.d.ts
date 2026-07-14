declare module "@tursodatabase/database" {
  export function connect(path: string): unknown;
  export function open(path: string): unknown;
  export class Database {
    constructor(path: string);
  }
}
