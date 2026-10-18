data "stripe_terminal_reader" "counter" {
  id = "tmr_123"
}

output "reader_status" {
  value = data.stripe_terminal_reader.counter.status
}
