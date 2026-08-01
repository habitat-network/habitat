export const truthy = <T>(x: T | null | undefined | ""): x is T => !!x;
