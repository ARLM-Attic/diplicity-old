window.PreferencesAllocationDialogView = BaseView.extend({

  template: _.template($('#preferences_allocation_dialog_underscore').html()),

	className: 'modal fade',

  events: {
		"hidden.bs.modal": "hide",
	  "click .preferences-done": "clickDone",
	},

	initialize: function(options) {
		this.gameState = options.gameState;
		this.cancel = options.cancel;
		this.done = options.done;
		this.nations = [];
		this.doneCalled = false;
		var that = this;
		_.each(variantMap[that.gameState.get('Variant')].Nations, function(nation) {
      that.nations.push(nation);
		});
	},

	clickDone: function() {
	  this.doneCalled = true;
		this.$el.modal('hide');
	},

	hide: function() {
		this.clean(true);
	  if (this.doneCalled) {
		  this.done(this.nations);
		} else {
			if (this.cancel != null) {
				this.cancel();
			}
		}
	},

	display: function() {
		$('body').append(this.doRender().el);
		this.$el.modal('show');
	},

  render: function() {
	  var that = this;
    that.$el.html(that.template({
		}));
		var update_list = null;
		update_list = function() {
			that.cleanChildren(true);
			_.each(that.nations, function(nation) {
				var nationView = new PreferredNationView({
					nation: nation,
					action: function() {
						for (var i = 0; i < that.nations.length; i++) {
							var found = that.nations[i];
							if (found == nation) {
								if (i > 0) {
									that.nations[i] = that.nations[i - 1];
									that.nations[i - 1] = found;
								}
								break;
							}
						}
						update_list();
					},
				}).doRender();
				that.$('.preferences-list').append(nationView.el);
				nationView.findParent();
			});
		};
		update_list();
		return that;
	},

});
