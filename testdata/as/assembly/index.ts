import {
  register,
  handleCall,
  hostCall,
  handleAbort,
  Result,
} from "@wapc/as-guest";

// Register Successful Function
register("echo", function(payload: ArrayBuffer): Result<ArrayBuffer> {
  // Callback with Payload
  hostCall("wapc", "testing", "echo", payload)
  return Result.ok(payload);
})

// Register Error Function
register("nope", function(payload: ArrayBuffer): Result<ArrayBuffer> {
  throw Result.error<ArrayBuffer>(new Error("No payload"));
})

// waPC boilerplate code
export function __guest_call(operation_size: usize, payload_size: usize): bool {
  return handleCall(operation_size, payload_size);
}

function abort(message: string | null, fileName: string | null, lineNumber: u32, columnNumber: u32): void {
  handleAbort(message, fileName, lineNumber, columnNumber);
}
