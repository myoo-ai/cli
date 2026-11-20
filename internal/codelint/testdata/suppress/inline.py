import os


def f():
    # myoo-ignore: step-comment
    # Step 1: suppressed by the directive above
    x = 1
    # Step 2: reported, the directive is two lines up
    print("✅ ok")  # myoo-ignore
    x += 1
    print("✅ no")  # myoo-ignore: emdash-in-code
    return x


def g():
    try:
        os.remove("x")
    # myoo-ignore: swallowed-exception, emoji-in-code
    except Exception:
        pass
