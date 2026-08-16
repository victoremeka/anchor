"""Errors raised by AnchorClient.

Errors are matched on the Engine's exact message text, not only on HTTP
status. Several distinct conditions share the same status code, for
example 404 for a missing task and 404 for an empty task queue. Matching
on message lets a caller catch the exact condition it cares about.
"""

from __future__ import annotations


class AnchorError(Exception):
    pass


class AnchorAPIError(AnchorError):
    def __init__(self, status_code: int, message: str):
        super().__init__(message)
        self.status_code = status_code
        self.message = message


class BadRequestError(AnchorAPIError):
    pass


class AuthenticationError(AnchorAPIError):
    pass


class NotFoundError(AnchorAPIError):
    pass


class ConflictError(AnchorAPIError):
    pass


class PayloadTooLargeError(AnchorAPIError):
    pass


class ServerError(AnchorAPIError):
    pass


class TaskNotAvailableError(NotFoundError):
    pass


class TaskNotFoundError(NotFoundError):
    pass


class AgentNotFoundError(NotFoundError):
    pass


class MemoryNotFoundError(NotFoundError):
    pass


class EffectNotFoundError(NotFoundError):
    pass


class APIKeyNotFoundError(NotFoundError):
    pass


class EffectReserveRaceError(ConflictError):
    pass


class LastActiveKeyError(ConflictError):
    pass


class UnregisteredTableError(BadRequestError):
    pass


_MESSAGE_TO_ERROR = {
    "no pending task available": TaskNotAvailableError,
    "task not found": TaskNotFoundError,
    "agent not found": AgentNotFoundError,
    "memory not found": MemoryNotFoundError,
    "no reserved effect found for idempotency key": EffectNotFoundError,
    "api key not found": APIKeyNotFoundError,
    "lost the race to reserve this effect": EffectReserveRaceError,
    "cannot revoke an organization's last active api key": LastActiveKeyError,
    "linked table not registered": UnregisteredTableError,
}

_STATUS_TO_ERROR = {
    400: BadRequestError,
    401: AuthenticationError,
    404: NotFoundError,
    409: ConflictError,
    413: PayloadTooLargeError,
}


def from_response(status_code: int, message: str) -> AnchorAPIError:
    cls = _MESSAGE_TO_ERROR.get(message)
    if cls is None:
        cls = _STATUS_TO_ERROR.get(status_code, ServerError if status_code >= 500 else AnchorAPIError)
    return cls(status_code, message)
