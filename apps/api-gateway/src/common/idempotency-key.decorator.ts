import {
  createParamDecorator,
  ExecutionContext,
  HttpStatus,
} from '@nestjs/common';
import { ApiError } from '../errors/api-error';

export const IdempotencyKey = createParamDecorator(
  (_data: unknown, context: ExecutionContext): string => {
    const request = context.switchToHttp().getRequest<{
      headers: Record<string, string | string[] | undefined>;
      rawHeaders: string[];
    }>();
    const key = request.headers['idempotency-key'];
    const count = request.rawHeaders.filter(
      (value, index) =>
        index % 2 === 0 && value.toLowerCase() === 'idempotency-key',
    ).length;
    if (
      count !== 1 ||
      typeof key !== 'string' ||
      !/^[A-Za-z0-9_-]{1,128}$/.test(key)
    ) {
      throw new ApiError(HttpStatus.BAD_REQUEST, [
        {
          code: 'INVALID_ARGUMENT',
          field: 'idempotencyKey',
          message:
            'Supply one Idempotency-Key containing 1–128 letters, digits, underscores, or hyphens.',
        },
      ]);
    }
    return key;
  },
);
