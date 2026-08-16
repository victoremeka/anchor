"""Pluggable stand ins for the LLM calls the product spec assigns to Bedrock.

classify_intent and draft_reply are simple rule based functions so the demo
runs without cloud credentials. A production deployment would call Bedrock
here with the same function signature.
"""

STATUS_INQUIRY = "status_inquiry"
REFUND_REQUEST = "refund_request"
GENERAL = "general"

_REFUND_WORDS = ("refund", "damaged", "broken", "return")
_STATUS_WORDS = ("where", "status", "when", "arrive", "tracking")


def classify_intent(message: str) -> str:
    lowered = message.lower()
    if any(word in lowered for word in _REFUND_WORDS):
        return REFUND_REQUEST
    if any(word in lowered for word in _STATUS_WORDS):
        return STATUS_INQUIRY
    return GENERAL


def draft_reply(order_id: str, intent: str, live_status: str) -> tuple:
    if intent == REFUND_REQUEST:
        subject = f"Your refund request for order {order_id}"
        if live_status == "refunded":
            body = (
                f"Your order {order_id} has already been refunded. "
                "The refund should appear on your original payment method within a few days."
            )
        else:
            body = (
                f"We received your refund request for order {order_id}. "
                "Our team will process it shortly."
            )
        return subject, body

    if intent == STATUS_INQUIRY:
        subject = f"Status of your order {order_id}"
        body = f"Order {order_id} is currently: {live_status}."
        return subject, body

    subject = f"Regarding your order {order_id}"
    body = f"Thanks for reaching out about order {order_id}. Our team will follow up shortly."
    return subject, body
