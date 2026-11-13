// client.commands: the bot's slash commands.
import type { RestClient } from '../rest/client.ts'
import type { Command, CommandInput } from '../types/commands.ts'

export class CommandManager {
  private readonly rest: RestClient

  constructor(rest: RestClient) {
    this.rest = rest
  }

  /** Replaces all commands (PUT /commands); commands an admin disabled stay. */
  set(commands: CommandInput[]): Promise<Command[]> {
    return this.rest.setCommands(commands)
  }

  /** Adds a command, or updates the one with the same name (POST /commands). */
  create(command: CommandInput): Promise<Command> {
    return this.rest.createCommand(command)
  }

  list(): Promise<Command[]> {
    return this.rest.listCommands()
  }

  delete(id: string): Promise<void> {
    return this.rest.deleteCommand(id)
  }
}
