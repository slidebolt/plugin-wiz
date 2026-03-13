Feature: WiZ bulb discovery

  Scenario: Plugin discovers a simulated bulb and registers it
    Given the wiz plugin starts with mock device "192.168.1.50:aabbccddeeff"
    Then the plugin "plugin-wiz" should register with the gateway
    And the device "wiz-aabbccddeeff" should appear under plugin "plugin-wiz"
    And the device should have a "light" entity

  Scenario: Plugin re-registers known bulbs on restart
    Given the wiz plugin starts with mock device "192.168.1.50:aabbccddeeff"
    And the plugin "plugin-wiz" should register with the gateway
    When the plugin is restarted
    Then the device "wiz-aabbccddeeff" should still be registered
