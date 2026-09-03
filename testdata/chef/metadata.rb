name "example_cookbook"
version('2.4.1')
license 'Apache-2.0' # SPDX expression

depends "apt"
depends('ntp', '~> 3.0')
depends "users", ">= 5.0", "< 9.0"
