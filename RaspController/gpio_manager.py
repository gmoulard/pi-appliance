#!/usr/bin/python3
#
#--------------------------------------
#
# GpioManager
#
# Author   : Ettore Gallina
# Date     : 20/12/2023
# Copyright: Egal Net di Ettore Gallina
#
# https://www.egalnetsoftwares.com/
#
#--------------------------------------


#Install gpiod with: sudo apt install python3-libgpiod

try:
    import gpiod
    _has_gpiod_lib = True
except ImportError:
    _has_gpiod_lib = False
import re
from RPi import GPIO


def _print_gpiod_chips():
    if _has_gpiod_lib:
        for chip in gpiod.ChipIter():
            print(chip)


class GpioManager:

    DIRECTION_IN = 1
    DIRECTION_OUT = 2

    LOW = 0
    HIGH = 1

    PULL_NONE = 0
    PULL_DOWN = 1
    PULL_UP = 2

    _CONSUMER = "RaspController"

    _is_gpiod = False


    def __init__(self):
        if _has_gpiod_lib:
            model = self._get_board_model()
            if model.startswith("Raspberry Pi 5"):
                #Raspberry Pi 5 uses gpiochip4, Raspberry Pi 4 uses uses gpiochip0, Raspberry Pi 3 not work correctly with gpiod
                #use the _print_gpiod_chips() function so see gpiochip number
                chip_num = 4
                self._is_gpiod = True
            elif model.startswith("Raspberry Pi 4"):
                chip_num = 0
                self._is_gpiod = True

        if self._is_gpiod:
            if hasattr(gpiod, "Chip"):
                self._chip = gpiod.Chip(str(chip_num), gpiod.Chip.OPEN_BY_NUMBER)
            else:
                self._chip = gpiod.chip(str(chip_num), gpiod.chip.OPEN_BY_NUMBER)
        else:
            GPIO.setmode(GPIO.BCM)
            GPIO.setwarnings(False)


    def _get_board_model(self):
        with open('/proc/cpuinfo', 'r') as cpu_info_file:
            cpu_info_content = cpu_info_file.read()
        match = re.search('^Model\s*:\s+(.+)$', cpu_info_content, flags=re.MULTILINE | re.IGNORECASE)
        if not match:
            return None
        else:
            return match.group(1)


    def _get_board_hardware(self):
        with open('/proc/cpuinfo', 'r') as cpu_info_file:
            cpu_info_content = cpu_info_file.read()
        match = re.search('^Hardware\s*:\s+(.+)$', cpu_info_content, flags=re.MULTILINE | re.IGNORECASE)
        if not match:
            return None
        else:
            return match.group(1)


    def _get_lines(self, pins):
        if isinstance(pins, list):
            return self._chip.get_lines(pins)
        else:
            return self._chip.get_line(pins)


    def _foreach_pin(self, pins, func):
        if isinstance(pins, list):
            for pin in pins:
                func(pin)
        else:
            func(pins)


    def set_direction(self, pins, direction):
        if self._is_gpiod:
            lines = self._get_lines(pins)
            if direction == self.DIRECTION_IN:
                self.set_input_pull(pins, None)
            elif direction == self.DIRECTION_OUT:
                lines.release()
                if hasattr(gpiod, "LINE_REQ_DIR_OUT"):
                    lines.request(consumer=self._CONSUMER, type=gpiod.LINE_REQ_DIR_OUT)
                else:
                    config = gpiod.line_request()
                    config.consumer = self._CONSUMER
                    config.request_type = gpiod.line_request.DIRECTION_OUTPUT
                    lines.request(config)
            else:
                raise RuntimeError("Invalid direction: %s" % direction)
        else:
            if direction == self.DIRECTION_IN:
                self._foreach_pin(pins, lambda pin: GPIO.setup(pin, GPIO.IN))
            elif direction == self.DIRECTION_OUT:
                self._foreach_pin(pins, lambda pin: GPIO.setup(pin, GPIO.OUT))
            else:
                raise RuntimeError("Invalid direction: %s" % direction)


    def set_output_value(self, pins, value):
        if value not in (self.LOW, self.HIGH):
            raise RuntimeError("Invalid value: %s" % value)

        if self._is_gpiod:
            if isinstance(pins, list):
                lines = self._chip.get_lines(pins)
                lines.set_values([value] * len(pins))
            else:
                line = self._chip.get_line(pins)
                line.set_value(value)
        else:
            self._foreach_pin(pins, lambda pin: GPIO.output(pin, GPIO.HIGH if value == self.HIGH else GPIO.LOW))


    def set_input_pull(self, pins, pull):
        if self._is_gpiod:
            flags = 0
            if pull is None:
                pass
            elif pull == self.PULL_UP:
                if hasattr(gpiod, "LINE_REQ_FLAG_BIAS_PULL_UP"):
                    flags |= gpiod.LINE_REQ_FLAG_BIAS_PULL_UP
                else:
                    raise NotImplementedError(
                        "Internal pullups not supported in this version of libgpiod, "
                        "use physical resistor instead!"
                    )
            elif pull == self.PULL_DOWN:
                if hasattr(gpiod, "line") and hasattr(
                    gpiod, "LINE_REQ_FLAG_BIAS_PULL_DOWN"
                ):
                    flags |= gpiod.LINE_REQ_FLAG_BIAS_PULL_DOWN
                else:
                    raise NotImplementedError(
                        "Internal pulldowns not supported in this version of libgpiod, "
                        "use physical resistor instead!"
                    )
            elif pull == self.PULL_NONE:
                if hasattr(gpiod, "line") and hasattr(
                    gpiod, "LINE_REQ_FLAG_BIAS_DISABLE"
                ):
                    flags |= gpiod.LINE_REQ_FLAG_BIAS_DISABLE
                else:
                    raise NotImplementedError(
                        "Internal pulldowns not supported in this version of libgpiod, "
                        "use physical resistor instead!"
                    )
            else:
                raise RuntimeError("Invalid pull: %s" % pull)

            lines = self._get_lines(pins)
            lines.release()
            if hasattr(gpiod, "LINE_REQ_DIR_IN"):
                lines.request(consumer=self._CONSUMER, type=gpiod.LINE_REQ_DIR_IN, flags=flags)
            else:
                config = gpiod.line_request()
                config.consumer = self._CONSUMER
                config.request_type = gpiod.line_request.DIRECTION_INPUT
                lines.request(config)
        else:
            if pull is None:
                self._foreach_pin(pins, lambda pin: GPIO.setup(pin, GPIO.IN))
            elif pull == self.PULL_UP:
                self._foreach_pin(pins, lambda pin: GPIO.setup(pin, GPIO.IN, GPIO.PUD_UP))
            elif pull == self.PULL_DOWN:
                self._foreach_pin(pins, lambda pin: GPIO.setup(pin, GPIO.IN, GPIO.PUD_DOWN))
            elif pull == self.PULL_NONE:
                self._foreach_pin(pins, lambda pin: GPIO.setup(pin, GPIO.IN, GPIO.PUD_OFF))
            else:
                raise RuntimeError("Invalid pull: %s" % pull)


    def read_input_value(self, pins):
        if self._is_gpiod:
            if isinstance(pins, list):
                lines = self._chip.get_lines(pins)
                return lines.get_values()
            else:
                line = self._chip.get_line(pins)
                return line.get_value()
        else:
            if isinstance(pins, list):
                values = []
                for pin in pins:
                    values.append(GPIO.input(pin))
                return values
            else:
                return GPIO.input(pins)


    def read_direction(self, pins):
        if isinstance(pins, list):
            directions = []
            for pin in pins:
                directions.append(self._read_direction_single_pin(pin))
            return directions
        else:
            return self._read_direction_single_pin(pins)


    def _read_direction_single_pin(self, pin):
        if self._is_gpiod:
            line = self._chip.get_line(pin)
            if line.direction() == gpiod.Line.DIRECTION_OUTPUT:
                return self.DIRECTION_OUT
            else:
                return self.DIRECTION_IN
        else:
            if GPIO.gpio_function(pin) == GPIO.IN:
                return self.DIRECTION_IN
            elif GPIO.gpio_function(pin) == GPIO.OUT:
                return self.DIRECTION_OUT
            else:
                raise RuntimeError("No direction for gpio: %s" % pin)


    def cleanup(self):
        if self._is_gpiod:
            self._chip.close()
        else:
            GPIO.cleanup()
