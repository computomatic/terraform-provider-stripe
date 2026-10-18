resource "stripe_terminal_configuration" "store" {
  name = "Store readers"

  offline = {
    enabled = true
  }

  reboot_window = {
    start_hour = 2
    end_hour   = 5
  }

  tipping = {
    usd = {
      percentages         = [10, 15, 20]
      fixed_amounts       = [100, 200, 300]
      smart_tip_threshold = 1000
    }
  }
}
