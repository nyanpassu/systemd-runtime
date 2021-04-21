```plain-txt
systemd_shim {
	launch {
		self.create()
		self.start()
	}
	run {
		launch {
			fifo(addr)
		}
		self.serve()
	}
}

runtime::create {
	let ta = factory.new()
	tasklist.add(wrap(&ta, &tasklist))
}

runtime::loadingTasks(ctx) {

}

runtime::loadTask(ctx) {
	
}

factory::new {

}

factory::existing {

}

factory::broken {

}

factory::delete {

}

volatile_task::connect {
	let addr = get_address()
	connect(addr, {
		self.lock {
			if self.disabled() {
				return
			}
			self.connect()
		}
	}) { ta ->
		self.lock {
			if self.killing() {
				ta.kill()
				return
			}
			self.set_task(ta)
		}
	}
}

volatile_task::get_status() {

}

volatile_task::kill {
	self.get_task_async().timeout(2s) { ta ->
		self.lock {
			ta.kill()
		}
	}.else {
		self.lock {
			if let Some(ta) = self.get_task() {
				ta.kill()
				// self.set_status(killed)
				return
			}
			if self.disabled {
				return
			}
			self.kill = true
		}
	}
}
```