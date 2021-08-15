import {
  register,
  handleCall,
  hostCall,
  handleAbort,
  consoleLog,
} from "wapc-guest-as";

export function wapc_init(): void {
  register("hello", hello);
  register("error", error);
}

function hello(payload: ArrayBuffer): ArrayBuffer {
  consoleLog("logging something")
  hostCall("myBinding", "sample", "hello", String.UTF8.encode("Simon"));
  const name = String.UTF8.decode(payload);
  return String.UTF8.encode("Hello, " + name);
}

function error(payload: ArrayBuffer): ArrayBuffer {
  throw new Error("error occurred")
}

// waPC boilerplate code below.  Do not remove.

export function __guest_call(operation_size: usize, payload_size: usize): bool {
  return handleCall(operation_size, payload_size);
}

function abort(
  message: string | null,
  fileName: string | null,
  lineNumber: u32,
  columnNumber: u32): void {
  handleAbort(message, fileName, lineNumber, columnNumber);
}