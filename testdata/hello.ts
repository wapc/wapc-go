import {
  register,
  handleCall,
  hostCall,
  handleAbort,
  consoleLog,
} from "wapc-guest-as";

export function _start(): void {
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

// This must be present in the entry file to be exported from the Wasm module.
export function __guest_call(operation_size: usize, payload_size: usize): bool {
  return handleCall(operation_size, payload_size);
}

// Abort function
function abort(message: string | null, fileName: string | null, lineNumber: u32, columnNumber: u32): void {
  handleAbort(message, fileName, lineNumber, columnNumber);
}