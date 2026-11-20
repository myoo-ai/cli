<?php

namespace App\Support;

const CURRENCY = 'EUR';

define('APP_VERSION', '1.2.0');

if (!function_exists('App\Support\format_price')) {
    /**
     * Formats cents with the currency symbol.
     */
    function format_price(int $cents, string $currency = CURRENCY): string
    {
        static $calls = 0;
        $calls++;
        return sprintf('%s %0.2f', $currency, $cents / 100);
    }
}

function version(): string
{
    return APP_VERSION . '/' . \PHP_VERSION;
}
