extern crate wapc_guest as guest;
use guest::prelude::*;

fn main() {}

#[no_mangle]
pub extern "C" fn wapc_init() {
  register_function("echo", hello);
  register_function("nope", fail);
}

fn hello(msg: &[u8]) -> CallResult {
  let _res = host_call("wapc", "testing", "echo", &msg.to_vec());
  Ok(msg.to_vec())
}

fn fail(_msg: &[u8]) -> CallResult {
  Err("Planned Failure".into())
}
