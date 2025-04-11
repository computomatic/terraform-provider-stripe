# Stripe provider for terraform

## Testing

```sh
% go install .

% echo <<EOF > ~/.terraformrc
provider_installation {
  dev_overrides {
      "github.com/computomatic/stripe" = "/Users/toby/go/bin"
  }

  # This is necessary to load other providers; otherwise, terraform will only use the dev_overrides block.
  direct {}
}
EOF

% STRIPE_API_KEY='<test-key>' ./test-provider.sh
```

## Build docs

```sh
% ./build-docs.sh
```
