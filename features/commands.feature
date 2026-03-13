Feature: WiZ light commands

  Background:
    Given the wiz plugin starts with mock device "192.168.1.50:aabbccddeeff"
    And the plugin "plugin-wiz" should register with the gateway
    And the device "wiz-aabbccddeeff" should have a "light" entity

  Scenario: Turn on a light
    When I send command "turn_on" to device "wiz-aabbccddeeff" entity "light"
    Then the command should succeed

  Scenario: Set RGB color
    When I send command "set_rgb" with rgb [255,0,128] to device "wiz-aabbccddeeff" entity "light"
    Then the command should succeed

  Scenario: Set brightness
    When I send command "set_brightness" with brightness 75 to device "wiz-aabbccddeeff" entity "light"
    Then the command should succeed

  Scenario: Reject invalid brightness
    When I send command "set_brightness" with brightness 150 to device "wiz-aabbccddeeff" entity "light"
    Then the command should fail with "brightness must be between 0 and 100"
