extern crate wapc_guest as guest;
use guest::prelude::*;

fn main() {}

#[no_mangle]
pub extern "C" fn wapc_init() {
  // Register echo and nope functions
  register_function("echo", hello);
  register_function("nope", fail);
}

// hello will callback the host and return the payload
fn hello(msg: &[u8]) -> CallResult {
  let _res = host_call("wapc", "testing", "echo", &msg.to_vec());
  Ok(msg.to_vec())
}

// fail will return an error result
fn fail(_msg: &[u8]) -> CallResult {
  Err("Planned Failure".into())
}
