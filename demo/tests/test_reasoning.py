from anchor_demo.reasoning import GENERAL, REFUND_REQUEST, STATUS_INQUIRY, classify_intent, draft_reply


def test_classify_refund_request():
    assert classify_intent("This arrived damaged, I want a refund") == REFUND_REQUEST


def test_classify_status_inquiry():
    assert classify_intent("Where is my order?") == STATUS_INQUIRY


def test_classify_general():
    assert classify_intent("Thanks for the great service") == GENERAL


def test_draft_reply_status_inquiry_reflects_live_status():
    subject, body = draft_reply("order-1", STATUS_INQUIRY, "shipped")
    assert "order-1" in subject
    assert "shipped" in body


def test_draft_reply_refund_already_refunded_says_so():
    subject, body = draft_reply("order-1", REFUND_REQUEST, "refunded")
    assert "already been refunded" in body


def test_draft_reply_refund_still_processing():
    subject, body = draft_reply("order-1", REFUND_REQUEST, "processing")
    assert "process it shortly" in body
