# ADR-086 — Local authorization is UID- and operation-bound

FutureDiff authorizes kernel-authenticated Unix UIDs against canonical API operation IDs. The default is deny. Path strings and user-supplied role claims are not trusted.
