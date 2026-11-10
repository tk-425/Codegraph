def outer():
    def inner():
        target()
    inner()


def target():
    pass
