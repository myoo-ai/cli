<?php
/**
 * Copyright 2026 Example Corp. Licensed under the MIT license.
 */

declare(strict_types=1);

namespace Fixture;

use Psr\Log\LoggerInterface;

abstract class Shape
{
    abstract public function area(): float;

    public function perimeter(): float
    {
        throw new \LogicException('Not implemented');
    }
}

final class Negative
{
    public function __construct(private LoggerInterface $logger)
    {
    }

    /**
     * Deletes the file at path. A missing file is not an error.
     */
    public function delete(string $path): void
    {
        try {
            unlink($path);
        } catch (\ErrorException $e) {
        }
        try {
            unlink($path);
        } catch (\Throwable $e) {
            // The cache entry is rebuilt on the next request anyway.
        }
    }

    /** @phpstan-ignore-next-line argument.type the legacy API takes a string id */
    public function legacy(int $unused): void
    {
        // phpcs:ignore Generic.Files.LineLength -- generated URL that cannot be split
        $this->logger->info(sprintf('%s', 'https://example.com'));
    }

    #[\Deprecated]
    public function attribute(): void
    {
        #[\SensitiveParameter]
        $fn = static fn(string $secret): string => sprintf('%05.2f %1$s', 1.5);
        printf("%d items\n", 3);
        $name = 'x';
        echo sprintf("$name has no conversion of its own");
    }
}

final class LanguageRules
{
    public function rules(array $items, ?object $user, string $name, callable $callback, array $args): mixed
    {
        $same = $name === $user->name;
        $loose = $user == null || 1 == 1;
        $offset = strpos($name, 'x', 2) !== false;
        $position = strpos($name, 'ab') === 3;
        $prefix = substr($name, 0, 4) === 'abc';
        $value = isset($items['k']) ? $items['other'] : 'none';
        $name = $name ?? 'anonymous' ?: 'x';
        $other = $items['k'] ?? 'none';
        $dir = __DIR__;
        $class = $user::class;
        $named = get_class(new \stdClass());
        $n = count($items);
        $exported = print_r($items, true);
        $code = var_export($items, true);
        $email = $user?->email;
        $phone = $this->find() !== null ? $this->find()->phone : null;
        $result = $callback(1, 2);
        $method = call_user_func([$this, 'find']);
        [$a, $b] = $items;
        $base = intval($name, 16);
        $pushed = array_push($items, $name, $a);
        $square = $n ** 2;
        $isNull = $user === null;
        switch ($n) {
            case 1:
                return 'one';
            case 2:
                $this->find();
                return 'two';
            default:
                return 'many';
        }
    }

    public function noDefault(int $n): string
    {
        switch ($n) {
            case 1:
                return 'one';
            case 2:
                return 'two';
        }
        return 'many';
    }

    private function find(): ?object
    {
        return null;
    }
}
